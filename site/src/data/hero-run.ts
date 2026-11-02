// Build-time loader for the hero replay. Reads the committed fixtures that
// scripts/capture-demo.sh recorded (`make capture`) and shapes them into the
// Monitor stage's props. Imported only from pages, never from an island.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { StageProps, TimedLine } from "../components/landing/MonitorStage";
import { buildTimeline, parseSnapshots, type Timeline, thin } from "../lib/monitor/timeline";

export type HeroRun = Omit<StageProps, "timeline"> & { timeline: Timeline };

/** What `make capture` recorded; keep in step with CAPTURE_PROJECT and CAPTURE_HOSTS. */
export const HERO_RUN = {
  project: "OpenData",
  task: "test",
  hosts: ["mbp", "m4-mini", "m1-mini"],
};

const FIXTURES = join(process.cwd(), "fixtures");
/** About how long one pass of the replay should take. */
const TARGET_REPLAY_MS = 45_000;
/** Island payload budget: more frames than this get thinned (transitions kept). */
const MAX_FRAMES = 180;

type RunEvent = {
  type: string;
  phase?: string;
  status?: string;
  ts?: string;
  [key: string]: unknown;
};

export function eventTone(event: RunEvent): TimedLine["tone"] {
  if (event.status === "failure" || event.status === "failed" || event.status === "error")
    return "fail";
  if (event.type === "result" && event.status === "success") return "ok";
  if (event.status === "warn" || event.status === "waiting") return "warn";
  return undefined;
}

export function prettyTone(line: string): TimedLine["tone"] {
  if (/✕|\bFAIL(ED)?\b|\berror\b/i.test(line)) return "fail";
  if (/^\s*◉/.test(line)) return "ok";
  if (/⚠|\bwarn/i.test(line)) return "warn";
  return undefined;
}

export function loadHeroRun(dir = FIXTURES): HeroRun {
  const snapshots = parseSnapshots(readFileSync(join(dir, "monitor.jsonl"), "utf8"));
  const timeline = thin(buildTimeline(snapshots, HERO_RUN.hosts), MAX_FRAMES);
  const start = Date.parse(snapshots[0]?.timestamp ?? "");

  const events = readFileSync(join(dir, "run.jsonl"), "utf8")
    .split("\n")
    .filter((line) => line.startsWith("{"))
    .map((text) => {
      const event = JSON.parse(text) as RunEvent;
      const at = event.ts ? Date.parse(event.ts) - start : timeline.durationMs;
      return { atMs: Math.max(0, at), text, tone: eventTone(event) };
    });

  const pretty = readFileSync(join(dir, "run.pretty.txt"), "utf8")
    .replace(/\n+$/, "")
    .split("\n")
    .map((text) => ({ atMs: 0, text, tone: prettyTone(text) }));

  return {
    timeline,
    events,
    pretty,
    task: HERO_RUN.task,
    project: HERO_RUN.project,
    speed: Math.max(1, Math.round(timeline.durationMs / TARGET_REPLAY_MS)),
  };
}
