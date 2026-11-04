// The hero island: one real `rr test` run replayed from two sides. On top, a
// recreation of `rr monitor` built from sampled `rr monitor --once --json`
// snapshots; underneath, the run's own output as a human (--pretty) or an agent
// (JSON events) sees it. Server-rendered with the busiest frame, so the page
// shows hosts at work without JavaScript or with reduced motion.
import { type KeyboardEvent, useEffect, useId, useMemo, useRef, useState } from "react";
import { alertFiring, THRESHOLDS } from "../../lib/monitor/render";
import {
  busiestFrame,
  history,
  type PackedTimeline,
  type Timeline,
  unpackTimeline,
} from "../../lib/monitor/timeline";
import { usePlayback, useTicker } from "../../lib/use-playback";
import { MonitorCard } from "./monitor/MonitorCard";

export type TimedLine = { atMs: number; text: string; tone?: "ok" | "fail" | "warn" | "muted" };

export type StageProps = {
  /**
   * The replay. The page passes it packed (packTimeline): column-wise arrays keep
   * the island's props a fraction of the size of per-frame objects.
   */
  timeline: Timeline | PackedTimeline;
  /** Agent view: the run's JSON events, timed against the timeline. */
  events: TimedLine[];
  /** Human view: a --pretty run of the same task, revealed in step with the replay. */
  pretty: TimedLine[];
  task: string;
  project: string;
  /** Replay speed; the label says it so durations stay honest. */
  speed: number;
};

const DEFAULT_COLS = 34;

function frameAt(timeline: Timeline, elapsed: number): number {
  let index = 0;
  timeline.frames.forEach((frame, i) => {
    if (frame.atMs <= elapsed) index = i;
  });
  return index;
}

const toneClass = {
  ok: "text-success",
  fail: "text-error",
  warn: "text-waiting",
  muted: "text-muted",
};

function useCardCols(ref: React.RefObject<HTMLDivElement | null>) {
  const [cols, setCols] = useState(DEFAULT_COLS);
  useEffect(() => {
    const node = ref.current;
    if (!node || typeof ResizeObserver === "undefined") return;
    const probe = document.createElement("span");
    probe.textContent = "0000000000";
    probe.className = "font-mono text-[12px] absolute invisible whitespace-pre";
    node.append(probe);
    const measure = () => {
      // The first card on screen (phones hide all but one).
      const card = [...node.querySelectorAll("article")].find((article) => article.clientWidth > 0);
      const charWidth = probe.getBoundingClientRect().width / 10;
      if (!card || charWidth === 0) return;
      // 24px of card padding, 2px of border.
      setCols(Math.max(14, Math.floor((card.clientWidth - 26) / charWidth)));
    };
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    measure();
    return () => {
      observer.disconnect();
      probe.remove();
    };
  }, [ref]);
  return cols;
}

export function MonitorStage({
  timeline: input,
  events,
  pretty,
  task,
  project,
  speed,
}: StageProps) {
  const timeline = useMemo<Timeline>(
    () => ("series" in input ? unpackTimeline(input) : input),
    [input],
  );
  const id = useId();
  const playback = usePlayback(timeline.durationMs, speed);
  const spin = useTicker(playback.playing);
  const gridRef = useRef<HTMLDivElement>(null);
  const cols = useCardCols(gridRef);
  const [view, setView] = useState<"human" | "agent">("human");
  const [mobileHost, setMobileHost] = useState(0);
  const busiest = useMemo(() => busiestFrame(timeline), [timeline]);

  const firing = useMemo(
    () =>
      Object.fromEntries(
        timeline.hosts.map((host) => {
          const series = (metric: "cpu" | "ram" | "gpu") =>
            timeline.frames.map((frame) => frame.hosts[host]?.[metric] ?? 0);
          const cpu = alertFiring(series("cpu"), THRESHOLDS.cpu);
          const ram = alertFiring(series("ram"), THRESHOLDS.ram);
          const gpu = alertFiring(series("gpu"), THRESHOLDS.gpu);
          return [host, cpu.map((value, i) => value || ram[i] || gpu[i])];
        }),
      ) as Record<string, boolean[]>,
    [timeline],
  );

  const started = playback.playing || playback.paused || playback.elapsed > 0;
  const index = playback.still || !started ? busiest : frameAt(timeline, playback.elapsed);
  const frame = timeline.frames[index];
  const shownAt = playback.still || !started ? (frame?.atMs ?? 0) : playback.elapsed;
  const progress = timeline.durationMs > 0 ? shownAt / timeline.durationMs : 1;

  const online = timeline.hosts.filter((host) => frame?.hosts[host]?.status !== "offline").length;
  const alerts = timeline.hosts.filter((host) => firing[host]?.[index]).length;
  const lines =
    view === "agent"
      ? events.filter((line) => line.atMs <= shownAt)
      : pretty.slice(0, Math.ceil(pretty.length * Math.min(1, progress)));
  // Reduced motion and the server render show the whole transcript.
  const everything = playback.complete || playback.still;
  const shownLines = everything ? (view === "agent" ? events : pretty) : lines;

  const transcriptRef = useRef<HTMLElement>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll when lines are added
  useEffect(() => {
    const node = transcriptRef.current;
    if (node && started) node.scrollTop = node.scrollHeight;
  }, [shownLines.length, started]);

  const onHostKey = (event: KeyboardEvent) => {
    const step = { ArrowRight: 1, ArrowLeft: -1 }[event.key];
    if (!step) return;
    const next = (mobileHost + step + timeline.hosts.length) % timeline.hosts.length;
    setMobileHost(next);
    document.getElementById(`${id}-host-${next}`)?.focus();
  };

  if (!frame) return null;

  return (
    <div ref={playback.ref} className="flex flex-col gap-3">
      <p className="sr-only">
        A replay of a real rr {task} run in the {project} project across {timeline.hosts.length}{" "}
        machines, shown in rr monitor: each host goes from idle to running a test suite, its CPU and
        memory climb, and it returns to idle when the suite finishes.
      </p>
      <div className="term term-hero">
        <div className="term-bar justify-between">
          <span className="min-w-0 truncate whitespace-pre">
            <span className="text-accent font-bold">rr monitor</span>
            <span className="text-ink-2">
              {" "}
              | {timeline.hosts.length} hosts | {online} online | last update just now
            </span>
            {alerts > 0 && (
              <span className="text-accent font-bold">
                {" "}
                {alerts} {alerts === 1 ? "alert" : "alerts"}
              </span>
            )}
          </span>
          <span className="hidden items-center gap-2 sm:flex">
            <span className="text-muted">{speed > 1 ? `replaying at ${speed}x` : "replay"}</span>
            <PlaybackButtons playback={playback} />
          </span>
        </div>

        {/* Phones: one card at a time, picked here; every host's status shows on its button. */}
        <div role="tablist" aria-label="Hosts" className="flex gap-1 px-3 pt-3 sm:hidden">
          {timeline.hosts.map((host, i) => {
            const status = frame.hosts[host]?.status ?? "offline";
            return (
              <button
                key={host}
                id={`${id}-host-${i}`}
                type="button"
                role="tab"
                aria-selected={i === mobileHost}
                aria-controls={`${id}-host-${i}-panel`}
                tabIndex={i === mobileHost ? 0 : -1}
                onClick={() => setMobileHost(i)}
                onKeyDown={onHostKey}
                className="rounded-control border-line aria-selected:border-line-strong aria-selected:text-ink text-ink-2 flex-1 border px-2 py-1.5 font-mono text-[11px]"
              >
                <span
                  aria-hidden="true"
                  className={
                    status === "running"
                      ? "text-waiting"
                      : status === "idle"
                        ? "text-info"
                        : "text-accent"
                  }
                >
                  {status === "running" ? "⣿" : status === "idle" ? "◉" : "◌"}
                </span>{" "}
                {host}
              </button>
            );
          })}
        </div>
        <div ref={gridRef} className="relative grid gap-3 p-3 sm:grid-cols-2 lg:grid-cols-3">
          {timeline.hosts.map((host, i) => (
            <div
              key={host}
              id={`${id}-host-${i}-panel`}
              role="tabpanel"
              aria-labelledby={`${id}-host-${i}`}
              className={i === mobileHost ? "" : "hidden sm:block"}
            >
              <HostCard
                timeline={timeline}
                host={host}
                index={index}
                cols={cols}
                spin={spin}
                firing={firing}
              />
            </div>
          ))}
        </div>
      </div>

      <div className="term">
        <div className="term-bar justify-between">
          <span className="text-ink-2 truncate">
            <span aria-hidden="true" className="text-accent">
              ${" "}
            </span>
            {view === "agent" ? `rr ${task}` : `rr --pretty ${task}`}
          </span>
          <div className="flex shrink-0 gap-1">
            <fieldset className="m-0 flex gap-1 border-0 p-0">
              <legend className="sr-only">Output for</legend>
              {(["human", "agent"] as const).map((option) => (
                <button
                  key={option}
                  type="button"
                  aria-pressed={view === option}
                  onClick={() => setView(option)}
                  className="rounded-control aria-pressed:bg-ink aria-pressed:text-canvas text-ink-2 border-line-strong border px-2 py-0.5 text-[11px]"
                >
                  {option === "human" ? "Human" : "Agent"}
                </button>
              ))}
            </fieldset>
            <span className="sm:hidden">
              <PlaybackButtons playback={playback} />
            </span>
          </div>
        </div>
        <section
          ref={transcriptRef}
          data-nosnippet
          // biome-ignore lint/a11y/noNoninteractiveTabindex: axe (scrollable-region-focusable) needs keyboard users able to scroll it
          tabIndex={0}
          aria-label={view === "agent" ? "JSON events" : "Pretty output"}
          className="term-scroll h-56 px-3 py-2 text-[12px]"
        >
          {shownLines.map((line, i) => (
            <div
              // biome-ignore lint/suspicious/noArrayIndexKey: transcript lines are append-only
              key={i}
              className={line.tone ? toneClass[line.tone] : "text-ink-2"}
            >
              {line.text || " "}
            </div>
          ))}
        </section>
      </div>
      <p className="text-muted text-xs">
        A real run of <code className="text-ink-2">rr {task}</code> in {project}, sampled with{" "}
        <code className="text-ink-2">rr monitor --once --json</code> (alerts enabled). The Human
        view is a <code className="text-ink-2">--pretty</code> run of the same task.
      </p>
    </div>
  );
}

function HostCard({
  timeline,
  host,
  index,
  cols,
  spin,
  firing,
}: {
  timeline: Timeline;
  host: string;
  index: number;
  cols: number;
  spin: number;
  firing: Record<string, boolean[]>;
}) {
  const frame = timeline.frames[index]?.hosts[host];
  if (!frame) return null;
  return (
    <MonitorCard
      host={host}
      frame={frame}
      cols={cols}
      spin={spin}
      firing={Boolean(firing[host]?.[index])}
      history={{
        cpu: history(timeline, host, "cpu", index),
        ram: history(timeline, host, "ram", index),
        gpu: history(timeline, host, "gpu", index),
        latency: history(timeline, host, "latency", index),
      }}
    />
  );
}

function PlaybackButtons({ playback }: { playback: ReturnType<typeof usePlayback> }) {
  if (playback.still) return null;
  const button =
    "rounded-control border-line-strong text-ink-2 hover:text-ink border px-2 py-0.5 text-[11px]";
  return playback.complete ? (
    <button type="button" onClick={playback.replay} className={button}>
      Replay
    </button>
  ) : (
    <button type="button" onClick={playback.toggle} className={button}>
      {playback.paused ? "Play" : "Pause"}
    </button>
  );
}
