// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  FLOW_HOSTS,
  FLOW_RUNS,
  type FlowRun,
  flowSteps,
  POLL_MS,
  sceneAt,
  simulate,
  waitSpans,
} from "./flow";

const plain = (agent: string, arrive: number, testMs = 5000): FlowRun => ({
  agent,
  command: "rr run x",
  arrive,
  tasks: [{ name: "x", syncMs: 500, testMs }],
  result: { ok: true, text: "ok" },
});

const busy = (kind: string) => kind === "sync" || kind === "test" || kind === "hold";

describe("the diagram's simulation follows rr's host choice", () => {
  it("gives each plain run the first unlocked host, trying the locked ones on the way", () => {
    const segments = simulate([plain("a", 0), plain("b", 1000), plain("c", 2000)], 3);
    const locks = segments.filter((s) => s.kind === "probe-ok").map((s) => [s.run, s.host]);
    expect(locks).toEqual([
      [0, 0],
      [1, 1],
      [2, 2],
    ]);
    const refusals = segments.filter((s) => s.kind === "probe-locked" && s.run === 2);
    expect(refusals.map((s) => s.host)).toEqual([0, 1]);
  });

  it("makes a run wait when every host is locked and retries every POLL_MS", () => {
    const segments = simulate([plain("a", 0, 9000), plain("b", 100, 3000)], 1);
    const tries = segments.filter((s) => s.run === 1 && s.kind.startsWith("probe"));
    expect(tries.at(-1)?.kind).toBe("probe-ok");
    for (const [i, attempt] of tries.slice(1).entries()) {
      expect(attempt.start - (tries[i]?.end ?? 0)).toBe(POLL_MS);
    }
    const holderEnd = segments.find((s) => s.run === 0 && s.kind === "test")?.end ?? 0;
    expect(tries.at(-1)?.end).toBeGreaterThanOrEqual(holderEnd);
    expect(waitSpans(segments, 1)).toHaveLength(1);
  });

  it("locks a host per subtask of a parallel task and releases them all when the last ends", () => {
    const parallel: FlowRun = {
      ...plain("p", 0),
      tasks: [
        { name: "a", syncMs: 100, testMs: 1000 },
        { name: "b", syncMs: 100, testMs: 3000 },
      ],
    };
    const segments = simulate([parallel], 2);
    expect(segments.filter((s) => s.kind === "probe-ok").map((s) => s.host)).toEqual([0, 1]);
    const releases = segments.filter((s) => s.kind === "result").map((s) => s.start);
    expect(new Set(releases).size).toBe(1);
    // The shorter subtask's host stays locked, held, until the longer one ends.
    const hold = segments.find((s) => s.kind === "hold");
    expect(hold?.host).toBe(0);
    expect(hold?.end).toBe(releases[0]);
  });

  it("never puts two runs on one host at once", () => {
    const segments = simulate(FLOW_RUNS, FLOW_HOSTS.length);
    const locked = segments.filter((s) => busy(s.kind));
    for (const a of locked) {
      for (const b of locked) {
        if (a.run === b.run || a.host !== b.host) continue;
        expect(a.end <= b.start || b.end <= a.start).toBe(true);
      }
    }
  });
});

describe("the page's scenario", () => {
  const segments = simulate(FLOW_RUNS, FLOW_HOSTS.length);
  const lockOf = (run: number) => segments.find((s) => s.run === run && s.kind === "probe-ok");

  it("has one parallel run on every host while two others wait on the laptop", () => {
    const scene = sceneAt(segments, FLOW_RUNS, FLOW_HOSTS.length, 9000);
    expect(scene.hosts.map((host) => host.holder)).toEqual([0, 0, 0]);
    expect(scene.chips[1]?.state).toBe("waiting");
    expect(scene.chips[2]?.state).toBe("waiting");
    expect(scene.chips[1]?.slot).toBe(1);
    expect(scene.chips[3]?.visible).toBe(false);
  });

  it("hands the freed hosts to the waiting runs in the order the story tells", () => {
    const release = segments.find((s) => s.run === 0 && s.kind === "result")?.start ?? 0;
    expect(lockOf(1)?.host).toBe(0);
    expect(lockOf(1)?.start).toBeGreaterThanOrEqual(release);
    expect(lockOf(2)?.host).toBe(1);
    expect(lockOf(3)?.host).toBe(2);
    // Agent 4 never waits.
    expect(waitSpans(segments, 3)).toEqual([]);
  });

  it("ends with every agent's own result and no rr process left", () => {
    const end = Math.max(...segments.map((s) => s.end)) + 1000;
    const done = sceneAt(segments, FLOW_RUNS, FLOW_HOSTS.length, end);
    expect(done.results.map((result) => result?.ok)).toEqual(FLOW_RUNS.map((r) => r.result.ok));
    expect(done.chips.every((chip) => !chip.visible)).toBe(true);
    expect(done.hosts.every((host) => host.phase === "free")).toBe(true);
  });

  it("tells the story in order, each beat starting at its event", () => {
    const steps = flowSteps(segments, FLOW_RUNS, FLOW_HOSTS);
    expect(steps).toHaveLength(5);
    for (const [i, step] of steps.slice(1).entries()) {
      expect(step.at - (steps[i]?.at ?? 0)).toBeGreaterThanOrEqual(1500);
    }
    expect(steps[2]?.text).toContain(`every ${POLL_MS / 1000}s`);
    expect(steps[3]?.text).toContain(`moves down the list to ${FLOW_HOSTS[1]}`);
  });
});
