// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  buildTimeline,
  busiestFrame,
  history,
  packTimeline,
  parseSnapshots,
  type Snapshot,
  thin,
  unpackTimeline,
} from "./timeline";

const host = (name: string, cpu: number, lock?: string) => ({
  name,
  alias: `${name}-local`,
  online: true,
  latency_ms: 12,
  cpu: { percent: cpu, cores: 8, load: [1.5, 1, 1] },
  ram: { percent: 50, used_bytes: 4, total_bytes: 8 },
  ...(lock ? { lock: { command: lock, started: "2026-11-02T16:41:00Z" } } : {}),
});

const snapshots: Snapshot[] = [
  { timestamp: "2026-11-02T16:40:58Z", hosts: [host("a", 5), host("b", 4)] },
  { timestamp: "2026-11-02T16:41:00Z", hosts: [host("a", 80, "pytest tests/api"), host("b", 4)] },
  {
    timestamp: "2026-11-02T16:41:41Z",
    hosts: [host("a", 95, "pytest tests/api"), host("b", 60, "bun test")],
  },
  {
    timestamp: "2026-11-02T16:41:43Z",
    hosts: [host("a", 10), { name: "b", online: false, latency_ms: 0 }],
  },
];

describe("buildTimeline", () => {
  const timeline = buildTimeline(snapshots, ["b", "a"]);

  it("keeps the given host order and times frames from the first snapshot", () => {
    expect(timeline.hosts).toEqual(["b", "a"]);
    expect(timeline.frames.map((frame) => frame.atMs)).toEqual([0, 2000, 43000, 45000]);
    expect(timeline.durationMs).toBe(45000);
  });

  it("reads a held lock as running, with its command and how long it's been held", () => {
    const a = timeline.frames[2]?.hosts.a;
    expect(a?.status).toBe("running");
    expect(a?.command).toBe("pytest tests/api");
    expect(a?.heldMs).toBe(41000);
    expect(timeline.frames[3]?.hosts.a?.status).toBe("idle");
    expect(timeline.frames[3]?.hosts.b?.status).toBe("offline");
  });

  it("picks the frame with the most hosts running as the still frame", () => {
    expect(busiestFrame(timeline)).toBe(2);
  });

  it("among equally busy frames, picks the one nearest 60% through the run", () => {
    const long: Snapshot[] = Array.from({ length: 11 }, (_, i) => ({
      timestamp: new Date(Date.UTC(2026, 10, 2, 16, 41, i * 10)).toISOString(),
      hosts: [host("a", 50, i > 0 && i < 10 ? "go test ./..." : undefined)],
    }));
    expect(busiestFrame(buildTimeline(long))).toBe(6);
  });

  it("builds a host's history from online frames up to the current one", () => {
    expect(history(timeline, "a", "cpu", 2)).toEqual([5, 80, 95]);
    expect(history(timeline, "b", "cpu", 3)).toEqual([4, 4, 60]);
  });
});

describe("thin", () => {
  it("drops frames but never a status change", () => {
    const many: Snapshot[] = Array.from({ length: 40 }, (_, i) => ({
      timestamp: new Date(Date.UTC(2026, 10, 2, 16, 41, i)).toISOString(),
      hosts: [host("a", i, i >= 17 && i < 23 ? "go test ./..." : undefined)],
    }));
    const thinned = thin(buildTimeline(many), 10);
    const statuses = thinned.frames.map((frame) => frame.hosts.a?.status);
    expect(thinned.frames.length).toBeLessThan(many.length);
    expect(statuses).toContain("running");
    const atMs = thinned.frames.map((frame) => frame.atMs);
    expect(atMs).toContain(17000);
    expect(atMs).toContain(23000);
  });
});

describe("parseSnapshots", () => {
  it("reads one snapshot per line and skips blanks", () => {
    const text = `${JSON.stringify(snapshots[0])}\n\n${JSON.stringify(snapshots[1])}\n`;
    expect(parseSnapshots(text)).toHaveLength(2);
  });
});

describe("packTimeline", () => {
  it("round-trips through the packed form, rounded to what the cards show", () => {
    const timeline = buildTimeline(snapshots, ["a", "b"]);
    const back = unpackTimeline(JSON.parse(JSON.stringify(packTimeline(timeline))));
    expect(back.hosts).toEqual(timeline.hosts);
    expect(back.durationMs).toBe(timeline.durationMs);
    expect(back.frames.map((frame) => frame.hosts.a?.status)).toEqual(
      timeline.frames.map((frame) => frame.hosts.a?.status),
    );
    expect(back.frames[2]?.hosts.a).toMatchObject({
      command: "pytest tests/api",
      cpu: 95,
      heldMs: 41000,
    });
    expect(back.frames[3]?.hosts.b?.status).toBe("offline");
  });
});
