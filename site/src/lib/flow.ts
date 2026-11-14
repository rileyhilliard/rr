// The "How it works" diagram's timeline. Agents on one laptop each call rr; every
// call is its own rr process, and those processes coordinate only through the lock
// on each host. A plain run tries the locks in `hosts:` order and takes the first
// free host; when all are locked it waits on the laptop and retries every POLL_MS
// (roundRobinWait in internal/cli/loadbalance.go). A parallel task starts one
// worker per host, each holding its host's lock until the whole task ends
// (internal/parallel). This simulates the scenario once, in fixed steps, and the
// diagram draws any moment of it.

export type FlowTask = { name: string; syncMs: number; testMs: number };
export type FlowRun = {
  agent: string;
  /** What the agent typed. */
  command: string;
  arrive: number;
  /** One task is a plain run; several are a parallel task's subtasks, one per host. */
  tasks: FlowTask[];
  result: { ok: boolean; text: string };
};

export type SegmentKind = "probe-locked" | "probe-ok" | "sync" | "test" | "hold" | "result";
export type Segment = {
  run: number;
  host: number;
  kind: SegmentKind;
  start: number;
  end: number;
  /** The subtask, for a parallel run. */
  task?: string;
};

/** A run's first lock attempts are drawn slowly enough to follow. */
export const PROBE_MS = 600;
/** Retries go back to back, the way roundRobinWait tries every locked host. */
export const RETRY_PROBE_MS = 250;
export const POLL_MS = 2000;
export const RESULT_MS = 600;
const STEP_MS = 50;

export const FLOW_HOSTS = ["m4-mini", "m1-linux", "m1-mini"];

export const FLOW_RUNS: FlowRun[] = [
  {
    agent: "Agent 1",
    command: "rr test",
    arrive: 400,
    tasks: [
      { name: "test-api", syncMs: 900, testMs: 8400 },
      { name: "test-web", syncMs: 900, testMs: 8000 },
      { name: "test-jobs", syncMs: 900, testMs: 8200 },
    ],
    result: { ok: true, text: "3/3 passed" },
  },
  {
    agent: "Agent 2",
    command: 'rr run "pytest auth"',
    arrive: 4000,
    tasks: [{ name: "pytest", syncMs: 800, testMs: 5600 }],
    result: { ok: true, text: "86 passed" },
  },
  {
    agent: "Agent 3",
    command: 'rr run "bun test lib"',
    arrive: 5700,
    tasks: [{ name: "bun test", syncMs: 800, testMs: 4800 }],
    result: { ok: false, text: "1 failed" },
  },
  {
    agent: "Agent 4",
    command: 'rr run "go test ./..."',
    arrive: 15000,
    tasks: [{ name: "go test", syncMs: 800, testMs: 3800 }],
    result: { ok: true, text: "214 passed" },
  },
];

type Worker =
  | { kind: "probe"; host: number; start: number; ms: number }
  | { kind: "waiting"; next: number }
  | { kind: "busy"; host: number; until: number }
  | { kind: "holding"; host: number; since: number }
  | { kind: "done" };

/** Runs the scenario: every lock attempt, sync, test and result as timed segments. */
export function simulate(runs: FlowRun[], hostCount: number): Segment[] {
  const segments: Segment[] = [];
  const holder: (number | undefined)[] = Array(hostCount).fill(undefined);
  // A plain run has one worker that walks the hosts; a parallel run has one per host.
  const workers: Worker[][] = runs.map(() => []);
  const horizon = Math.max(...runs.map((run) => run.arrive)) + 120_000;
  const finished = () =>
    workers.every((mine) => mine.length > 0 && mine.every((w) => w.kind === "done"));

  for (let t = 0; t <= horizon && !finished(); t += STEP_MS) {
    runs.forEach((run, i) => {
      const parallel = run.tasks.length > 1;
      const mine = workers[i] as Worker[];
      if (mine.length === 0 && t >= run.arrive) {
        const count = parallel ? Math.min(run.tasks.length, hostCount) : 1;
        for (let w = 0; w < count; w++) {
          mine.push({ kind: "probe", host: parallel ? w : 0, start: t, ms: PROBE_MS });
        }
      }
      mine.forEach((worker, w) => {
        if (worker.kind === "probe" && t >= worker.start + worker.ms) {
          const free = holder[worker.host] === undefined;
          segments.push({
            run: i,
            host: worker.host,
            kind: free ? "probe-ok" : "probe-locked",
            start: worker.start,
            end: t,
          });
          if (free) {
            const task = run.tasks[parallel ? w : 0] as FlowTask;
            const name = parallel ? task.name : undefined;
            const syncEnd = t + task.syncMs;
            const until = syncEnd + task.testMs;
            holder[worker.host] = i;
            segments.push({
              run: i,
              host: worker.host,
              kind: "sync",
              start: t,
              end: syncEnd,
              task: name,
            });
            segments.push({
              run: i,
              host: worker.host,
              kind: "test",
              start: syncEnd,
              end: until,
              task: name,
            });
            mine[w] = { kind: "busy", host: worker.host, until };
          } else if (!parallel && worker.host + 1 < hostCount) {
            mine[w] = { ...worker, host: worker.host + 1, start: t };
          } else {
            // A parallel worker waits for its own host; a plain run for any host.
            mine[w] = { kind: "waiting", next: t + POLL_MS };
          }
        } else if (worker.kind === "waiting" && t >= worker.next) {
          mine[w] = { kind: "probe", host: parallel ? w : 0, start: t, ms: RETRY_PROBE_MS };
        } else if (worker.kind === "busy" && t >= worker.until) {
          mine[w] = { kind: "holding", host: worker.host, since: t };
        }
      });
      // Locks release when the run ends: a plain run's at once, a parallel task's
      // only when every subtask is done.
      if (mine.length > 0 && mine.every((w) => w.kind === "holding")) {
        for (const [w, worker] of mine.entries()) {
          if (worker.kind !== "holding") continue;
          if (worker.since < t) {
            segments.push({ run: i, host: worker.host, kind: "hold", start: worker.since, end: t });
          }
          holder[worker.host] = undefined;
          segments.push({
            run: i,
            host: worker.host,
            kind: "result",
            start: t,
            end: t + RESULT_MS,
          });
          (workers[i] as Worker[])[w] = { kind: "done" };
        }
      }
    });
  }
  return segments.sort((a, b) => a.start - b.start || a.host - b.host);
}

export type ChipView = {
  /** The rr process exists from the agent's call until its exit code is back. */
  visible: boolean;
  state: "trying" | "waiting" | "running" | "done";
  /** While waiting: milliseconds until it tries the locks again. */
  retryIn?: number;
  /** 0 in the running column, 1 in the waiting column; eased between them. */
  slot: number;
};
export type HostView = {
  holder?: number;
  task?: string;
  phase: "free" | "sync" | "test" | "hold";
  /** 0..1 through the current phase. */
  progress: number;
};
export type Tag = { host: number; run: number; ok: boolean; age: number; ms: number };
export type Scene = {
  chips: ChipView[];
  hosts: HostView[];
  /** Segments in flight at this moment, with how far along (0..1) each is. */
  active: { segment: Segment; at: number }[];
  /** Lock attempts that just landed, shown at the host end of the line. */
  tags: Tag[];
  results: (FlowRun["result"] | undefined)[];
};

const covers = (s: Segment, t: number) => t >= s.start && t < s.end;
const SLIDE_MS = 450;
export const TAG_MS = 1100;
const RETRY_TAG_MS = 600;
/** The rr process lingers this long after its exit code is back. */
const LINGER_MS = 900;

const ease = (u: number) => 1 - (1 - Math.max(0, Math.min(1, u))) ** 3;

/** The spans a run spends waiting on the laptop: from giving up until a retry lands a lock. */
export function waitSpans(segments: Segment[], run: number): [number, number][] {
  const probes = segments.filter((s) => s.run === run && s.kind.startsWith("probe"));
  const spans: [number, number][] = [];
  let since: number | undefined;
  for (const [k, s] of probes.entries()) {
    const next = probes[k + 1];
    if (s.kind === "probe-locked" && since === undefined && next && next.start > s.end) {
      since = s.end;
    }
    if (s.kind === "probe-ok" && since !== undefined) {
      spans.push([since, s.end]);
      since = undefined;
    }
  }
  return spans;
}

export function sceneAt(segments: Segment[], runs: FlowRun[], hostCount: number, t: number): Scene {
  const active = segments
    .filter((s) => covers(s, t) && s.kind !== "hold")
    .map((segment) => ({ segment, at: (t - segment.start) / (segment.end - segment.start) }));

  const hosts: HostView[] = Array.from({ length: hostCount }, (_, host) => {
    const lock = segments.find(
      (s) =>
        s.host === host &&
        (s.kind === "sync" || s.kind === "test" || s.kind === "hold") &&
        covers(s, t),
    );
    if (!lock) return { phase: "free", progress: 0 };
    return {
      holder: lock.run,
      task: lock.task,
      phase: lock.kind as HostView["phase"],
      progress: (t - lock.start) / (lock.end - lock.start),
    };
  });

  // A quick retry's answer shows briefly; a first attempt's long enough to read.
  const tagMs = (s: Segment) => (s.end - s.start < PROBE_MS ? RETRY_TAG_MS : TAG_MS);
  const tags = segments
    .filter((s) => s.kind.startsWith("probe") && t >= s.end && t < s.end + tagMs(s))
    .map((s) => ({
      host: s.host,
      run: s.run,
      ok: s.kind === "probe-ok",
      age: t - s.end,
      ms: tagMs(s),
    }));

  const chips: ChipView[] = runs.map((run, i) => {
    const mine = segments.filter((s) => s.run === i);
    const lastResult = mine.filter((s) => s.kind === "result").at(-1);
    const end = lastResult ? lastResult.end : Number.POSITIVE_INFINITY;
    const visible = t >= run.arrive && t < end + LINGER_MS;
    const spans = waitSpans(segments, i);
    // Slide toward the waiting column when a wait starts, back when it ends.
    let slot = 0;
    for (const [from, to] of spans) {
      if (t >= from && t < to) slot = ease((t - from) / SLIDE_MS);
      else if (t >= to && t < to + SLIDE_MS) slot = 1 - ease((t - to) / SLIDE_MS);
    }
    if (t >= end) return { visible, state: "done", slot };
    const busy = mine.some((s) => !s.kind.startsWith("probe") && covers(s, t));
    if (busy) return { visible, state: "running", slot };
    const waiting = spans.some(([from, to]) => t >= from && t < to);
    const probing = mine.some((s) => s.kind.startsWith("probe") && covers(s, t));
    if (waiting && !probing) {
      const next = mine.find((s) => s.start > t);
      return { visible, state: "waiting", retryIn: next ? next.start - t : 0, slot };
    }
    return { visible, state: waiting ? "waiting" : "trying", slot };
  });

  const results = runs.map((run, i) =>
    segments.some((s) => s.run === i && s.kind === "result" && t >= s.end) ? run.result : undefined,
  );
  return { chips, hosts, active, tags, results };
}

export type FlowStep = { at: number; title: string; text: string };

/** The story's beats, each starting at a moment the simulation produced. */
export function flowSteps(segments: Segment[], runs: FlowRun[], hosts: string[]): FlowStep[] {
  const lockOf = (run: number) => segments.find((s) => s.run === run && s.kind === "probe-ok");
  const release = segments.find((s) => s.run === 0 && s.kind === "result");
  const second = lockOf(1);
  const third = lockOf(2);
  const fourth = lockOf(runs.length - 1);
  // Agent 3's retry that finds the second agent's new host taken.
  const bounce = segments.find(
    (s) => s.run === 2 && s.kind === "probe-locked" && s.start >= (second?.end ?? 0),
  );
  const last = runs.length - 1;
  const name = (host: number | undefined) => hosts[host ?? 0] ?? "";
  return [
    {
      at: 0,
      title: `${runs[0]?.agent} takes every host`,
      text: `${runs[0]?.agent} runs \`${runs[0]?.command}\`, a parallel task. Its rr locks all ${hosts.length} hosts and gives each one a subtask.`,
    },
    {
      at: runs[1]?.arrive ?? 0,
      title: `${runs[1]?.agent} and ${runs[2]?.agent} wait`,
      text: `${runs[1]?.agent} and ${runs[2]?.agent} run their own tests. Each one's rr tries the hosts top to bottom, finds every lock taken, and waits on your laptop.`,
    },
    {
      at: release?.start ?? 0,
      title: `${runs[1]?.agent} gets ${name(second?.host)}`,
      text: `${runs[0]?.agent}'s subtasks finish and all ${hosts.length} locks release together. Each waiting rr checks again every ${POLL_MS / 1000}s on its own timer, with no shared queue, and ${runs[1]?.agent} checks first and takes ${name(second?.host)}.`,
    },
    {
      at: bounce?.start ?? 0,
      title: `${runs[2]?.agent} gets ${name(third?.host)}`,
      text: `${runs[2]?.agent} checks next, finds ${name(second?.host)} taken, and moves down the list to ${name(third?.host)}.`,
    },
    {
      at: runs[last]?.arrive ?? 0,
      title: `${runs[last]?.agent} gets ${name(fourth?.host)}`,
      text: `${runs[last]?.agent} arrives while a host is free and takes it without waiting. Each agent gets its own output and exit code.`,
    },
  ];
}
