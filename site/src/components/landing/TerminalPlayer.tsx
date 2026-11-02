// A scripted terminal for the Commands and Agent sections. Steps play on the
// shared playback clock: commands type out, output arrives line by line, and agent
// turns render the way Claude Code shows them (❯ prompt, Bash(...) calls, ⎿ output).
// Server-rendered and under reduced motion it shows the whole transcript.
import { useEffect, useRef } from "react";
import { usePlayback } from "../../lib/use-playback";

export type Tone = "ok" | "fail" | "warn" | "muted";
export type Line = { text: string; tone?: Tone };

export type Step =
  | { kind: "command"; text: string }
  | { kind: "output"; lines: Line[] }
  | { kind: "user"; text: string }
  | { kind: "agent"; text: string }
  | { kind: "tool"; name: string; arg: string }
  | { kind: "tool-output"; lines: Line[]; more?: number }
  | { kind: "note"; text: string };

const TYPE_MS = 28;
const LINE_MS = 45;
const WORD_MS = 55;
const PAUSE_MS = 500;

function stepDuration(step: Step): number {
  switch (step.kind) {
    case "command":
      return Math.min(step.text.length * TYPE_MS, 1600) + PAUSE_MS;
    case "user":
      return Math.min(step.text.length * TYPE_MS, 2000) + PAUSE_MS;
    case "agent":
      return step.text.split(/\s+/).length * WORD_MS + PAUSE_MS;
    case "tool":
      return 900;
    case "output":
    case "tool-output":
      return Math.min(step.lines.length * LINE_MS, 2400) + PAUSE_MS;
    case "note":
      return PAUSE_MS;
  }
}

/** Start time of each step, plus the script's total length. */
export function schedule(steps: readonly Step[]): { starts: number[]; total: number } {
  const starts: number[] = [];
  let at = 0;
  for (const step of steps) {
    starts.push(at);
    at += stepDuration(step);
  }
  return { starts, total: at };
}

const toneClass: Record<Tone, string> = {
  ok: "text-success",
  fail: "text-error",
  warn: "text-waiting",
  muted: "text-muted",
};

function Lines({ lines, prefix }: { lines: Line[]; prefix?: string }) {
  return (
    <>
      {lines.map((line, i) => (
        <div
          // biome-ignore lint/suspicious/noArrayIndexKey: output lines are a fixed recording
          key={i}
          className={line.tone ? toneClass[line.tone] : "text-ink-2"}
        >
          {prefix && (
            <span aria-hidden="true" className="text-muted">
              {i === 0 ? prefix : " ".repeat(prefix.length)}
            </span>
          )}
          {line.text || " "}
        </div>
      ))}
    </>
  );
}

function renderStep(step: Step, progress: number, key: number) {
  // progress: 0..1 through the step's active part (before its pause); 1 is fully shown.
  const chars = (text: string) =>
    progress >= 1 ? text : text.slice(0, Math.ceil(text.length * progress));
  const partialLines = (lines: Line[]) =>
    progress >= 1 ? lines : lines.slice(0, Math.ceil(lines.length * progress));
  switch (step.kind) {
    case "command":
      return (
        <div key={key} className="text-ink">
          <span aria-hidden="true" className="text-accent">
            ${" "}
          </span>
          {chars(step.text)}
        </div>
      );
    case "output":
      return (
        <div key={key}>
          <Lines lines={partialLines(step.lines)} />
        </div>
      );
    case "user":
      return (
        <div
          key={key}
          className="text-ink bg-canvas/60 rounded-control -mx-1 my-1 px-1 whitespace-pre-wrap"
        >
          <span aria-hidden="true" className="text-muted">
            ❯{" "}
          </span>
          {chars(step.text)}
        </div>
      );
    case "agent": {
      const words = step.text.split(/(\s+)/);
      const shown =
        progress >= 1 ? step.text : words.slice(0, Math.ceil(words.length * progress)).join("");
      return (
        <div key={key} className="text-ink my-1 whitespace-pre-wrap">
          <span aria-hidden="true" className="text-ink">
            ●{" "}
          </span>
          {shown}
        </div>
      );
    }
    case "tool":
      return (
        <div key={key} className="mt-1">
          <span aria-hidden="true" className={progress >= 1 ? "text-success" : "text-waiting"}>
            ●{" "}
          </span>
          <span className="text-ink font-bold">{step.name}</span>
          <span className="text-ink-2">({step.arg})</span>
        </div>
      );
    case "tool-output":
      return (
        <div key={key} className="mb-1">
          <Lines lines={partialLines(step.lines)} prefix="  ⎿  " />
          {step.more !== undefined && progress >= 1 && (
            <div className="text-muted">{`     … +${step.more} lines`}</div>
          )}
        </div>
      );
    case "note":
      return (
        <div key={key} className="text-muted">
          {step.text}
        </div>
      );
  }
}

type Props = {
  title: string;
  steps: Step[];
  /** Fixed transcript height so switching scripts never moves the page. */
  heightClass?: string;
  label: string;
};

export function TerminalPlayer({ title, steps, heightClass = "h-80", label }: Props) {
  const { starts, total } = schedule(steps);
  const playback = usePlayback(total);
  const bodyRef = useRef<HTMLElement>(null);
  const showAll = playback.still || playback.complete;
  const elapsed = showAll ? total : playback.elapsed;
  const visible = steps.filter((_, i) => (starts[i] ?? 0) <= elapsed);

  // biome-ignore lint/correctness/useExhaustiveDependencies: follow the newest output while playing
  useEffect(() => {
    const node = bodyRef.current;
    if (node && playback.playing) node.scrollTop = node.scrollHeight;
  }, [elapsed, playback.playing]);

  return (
    <div ref={playback.ref} className="term">
      <div className="term-bar justify-between">
        <span className="truncate">{title}</span>
        {!playback.still && (
          <button
            type="button"
            onClick={playback.complete ? playback.replay : playback.toggle}
            className="rounded-control border-line-strong text-ink-2 hover:text-ink shrink-0 border px-2 py-0.5 text-[11px]"
          >
            {playback.complete ? "Replay" : playback.paused ? "Play" : "Pause"}
          </button>
        )}
      </div>
      <section
        ref={bodyRef}
        // biome-ignore lint/a11y/noNoninteractiveTabindex: axe (scrollable-region-focusable) needs keyboard users able to scroll it
        tabIndex={0}
        aria-label={label}
        className={`term-scroll ${heightClass} px-3 py-2 text-[12px]`}
      >
        {visible.map((step, i) => {
          const start = starts[i] ?? 0;
          const progress = showAll
            ? 1
            : Math.min(1, (elapsed - start) / Math.max(1, stepDuration(step) - PAUSE_MS));
          return renderStep(step, progress, i);
        })}
      </section>
    </div>
  );
}
