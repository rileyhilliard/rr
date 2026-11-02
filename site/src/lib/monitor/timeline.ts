// Turns `rr monitor --once --json` snapshots (fixtures/monitor.jsonl, recorded by
// scripts/capture-demo.sh) into frames the Monitor stage replays. Pure: the page
// reads the fixture at build time and passes the result in.

export type HostSample = {
  name: string;
  alias?: string;
  online: boolean;
  error?: string;
  latency_ms: number;
  cpu?: { percent: number; cores: number; load: number[]; per_core?: number[] };
  ram?: { percent: number; used_bytes: number; total_bytes: number };
  gpu?: { name?: string; percent: number };
  disk?: { percent: number };
  net?: { rx_bytes_per_sec: number; tx_bytes_per_sec: number };
  lock?: { command: string; started?: string };
};

export type Snapshot = { timestamp: string; hosts: HostSample[] };

export type HostStatus = "idle" | "running" | "offline";

export type HostFrame = {
  status: HostStatus;
  alias: string;
  /** The lock holder's command while running. */
  command: string;
  /** How long the lock has been held at this frame, in ms. */
  heldMs: number;
  cpu: number;
  load1: number;
  /** Per-core CPU, when the host reports it (Linux); absent on macOS hosts. */
  perCore?: number[];
  ram: number;
  gpu: number | null;
  disk: number | null;
  latency: number;
  rx: number;
  tx: number;
};

export type Frame = { atMs: number; hosts: Record<string, HostFrame> };

export type Timeline = {
  hosts: string[];
  frames: Frame[];
  /** Wall-clock length of the recording. */
  durationMs: number;
};

export function parseSnapshots(jsonl: string): Snapshot[] {
  return jsonl
    .split("\n")
    .filter((line) => line.trim().startsWith("{"))
    .map((line) => JSON.parse(line) as Snapshot);
}

function hostFrame(sample: HostSample | undefined, at: number): HostFrame {
  if (!sample?.online) {
    return {
      status: "offline",
      alias: sample?.alias ?? "",
      command: "",
      heldMs: 0,
      cpu: 0,
      load1: 0,
      perCore: [],
      ram: 0,
      gpu: null,
      disk: null,
      latency: 0,
      rx: 0,
      tx: 0,
    };
  }
  const started = sample.lock?.started ? Date.parse(sample.lock.started) : Number.NaN;
  return {
    status: sample.lock ? "running" : "idle",
    alias: sample.alias ?? "",
    command: sample.lock?.command ?? "",
    heldMs: Number.isNaN(started) ? 0 : Math.max(0, at - started),
    cpu: sample.cpu?.percent ?? 0,
    load1: sample.cpu?.load[0] ?? 0,
    perCore: sample.cpu?.per_core ?? [],
    ram: sample.ram?.percent ?? 0,
    gpu: sample.gpu ? sample.gpu.percent : null,
    disk: sample.disk ? sample.disk.percent : null,
    latency: sample.latency_ms,
    rx: sample.net?.rx_bytes_per_sec ?? 0,
    tx: sample.net?.tx_bytes_per_sec ?? 0,
  };
}

/** Host order: the order the first snapshot lists them, which the stage keeps fixed. */
export function buildTimeline(
  snapshots: readonly Snapshot[],
  hostOrder?: readonly string[],
): Timeline {
  if (snapshots.length === 0) return { hosts: [], frames: [], durationMs: 0 };
  const start = Date.parse(snapshots[0]?.timestamp ?? "");
  const hosts = hostOrder ? [...hostOrder] : (snapshots[0]?.hosts.map((host) => host.name) ?? []);
  const frames = snapshots.map((snap) => {
    const at = Date.parse(snap.timestamp);
    const byName = new Map(snap.hosts.map((host) => [host.name, host]));
    return {
      atMs: at - start,
      hosts: Object.fromEntries(hosts.map((name) => [name, hostFrame(byName.get(name), at)])),
    };
  });
  return { hosts, frames, durationMs: frames.at(-1)?.atMs ?? 0 };
}

/**
 * Keeps at most `max` frames: every Nth one, plus every frame where a host's
 * status or command changes, so thinning never hides a transition.
 */
export function thin(timeline: Timeline, max: number): Timeline {
  const { frames, hosts } = timeline;
  if (frames.length <= max) return timeline;
  const step = Math.ceil(frames.length / max);
  const kept = frames.filter((frame, i) => {
    if (i % step === 0 || i === frames.length - 1) return true;
    const prev = frames[i - 1];
    return hosts.some(
      (host) =>
        prev?.hosts[host]?.status !== frame.hosts[host]?.status ||
        prev?.hosts[host]?.command !== frame.hosts[host]?.command,
    );
  });
  return { ...timeline, frames: kept };
}

/**
 * The frame shown without motion (server render, reduced motion): among frames
 * with the most hosts running, the one nearest 60% through the recording, so the
 * graphs have history behind them.
 */
export function busiestFrame(timeline: Timeline): number {
  const running = timeline.frames.map(
    (frame) => timeline.hosts.filter((host) => frame.hosts[host]?.status === "running").length,
  );
  const most = Math.max(0, ...running);
  const target = timeline.durationMs * 0.6;
  let best = 0;
  let bestDistance = Number.POSITIVE_INFINITY;
  timeline.frames.forEach((frame, i) => {
    if (running[i] !== most) return;
    const distance = Math.abs(frame.atMs - target);
    if (distance < bestDistance) {
      best = i;
      bestDistance = distance;
    }
  });
  return best;
}

type Metric = "cpu" | "ram" | "gpu" | "latency";

/** A host's metric history up to and including frame `index`, for its sparkline. */
export function history(timeline: Timeline, host: string, metric: Metric, index: number): number[] {
  const values: number[] = [];
  for (const frame of timeline.frames.slice(0, index + 1)) {
    const value = frame.hosts[host]?.[metric];
    if (typeof value === "number" && frame.hosts[host]?.status !== "offline") values.push(value);
  }
  return values;
}

const NUMERIC = ["heldMs", "cpu", "load1", "ram", "latency", "rx", "tx"] as const;
const NULLABLE = ["gpu", "disk"] as const;

/** Column-wise timeline: one array per host per field, so keys aren't repeated per frame. */
export type PackedTimeline = {
  hosts: string[];
  durationMs: number;
  atMs: number[];
  series: Record<string, Record<string, (string | number | null)[]>>;
};

const round = (key: string, value: number) =>
  ["rx", "tx", "heldMs", "latency"].includes(key) ? Math.round(value) : Math.round(value * 10) / 10;

/**
 * Packs a timeline for an island prop. Values are rounded to what the cards show,
 * and per-core lists are dropped (the cards don't draw them).
 */
export function packTimeline(timeline: Timeline): PackedTimeline {
  const series: PackedTimeline["series"] = {};
  for (const host of timeline.hosts) {
    const columns: Record<string, (string | number | null)[]> = {
      status: [],
      alias: [],
      command: [],
    };
    for (const key of [...NUMERIC, ...NULLABLE]) columns[key] = [];
    for (const frame of timeline.frames) {
      const h = frame.hosts[host];
      columns.status?.push(h?.status ?? "offline");
      columns.alias?.push(h?.alias ?? "");
      columns.command?.push(h?.command ?? "");
      for (const key of NUMERIC) columns[key]?.push(round(key, h?.[key] ?? 0));
      for (const key of NULLABLE) {
        const value = h?.[key];
        columns[key]?.push(value === null || value === undefined ? null : round(key, value));
      }
    }
    series[host] = columns;
  }
  return {
    hosts: timeline.hosts,
    durationMs: timeline.durationMs,
    atMs: timeline.frames.map((frame) => frame.atMs),
    series,
  };
}

export function unpackTimeline(packed: PackedTimeline): Timeline {
  const frames: Frame[] = packed.atMs.map((atMs, i) => ({
    atMs,
    hosts: Object.fromEntries(
      packed.hosts.map((host) => {
        const column = (key: string) => packed.series[host]?.[key]?.[i];
        const frame: HostFrame = {
          status: (column("status") as HostStatus) ?? "offline",
          alias: String(column("alias") ?? ""),
          command: String(column("command") ?? ""),
          heldMs: Number(column("heldMs") ?? 0),
          cpu: Number(column("cpu") ?? 0),
          load1: Number(column("load1") ?? 0),
          ram: Number(column("ram") ?? 0),
          gpu: column("gpu") === null ? null : Number(column("gpu")),
          disk: column("disk") === null ? null : Number(column("disk")),
          latency: Number(column("latency") ?? 0),
          rx: Number(column("rx") ?? 0),
          tx: Number(column("tx") ?? 0),
        };
        return [host, frame];
      }),
    ),
  }));
  return { hosts: packed.hosts, frames, durationMs: packed.durationMs };
}
