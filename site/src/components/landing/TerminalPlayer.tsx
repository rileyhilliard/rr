// A scripted terminal for the Commands and Agent sections. Steps play on
// @steamfield/ui's agent demo clock (useAgentDemo): commands type out, output arrives
// line by line, and agent turns render the way Claude Code shows them (❯ prompt,
// Bash(...) calls, ⎿ output). Server-rendered and under reduced motion it shows the
// whole transcript. The library owns timing and playback; this file keeps rr's markup.

import { useAgentDemo } from "@steamfield/ui/react/agent-demo";
import type { DemoScript, Step as DemoStep } from "@steamfield/ui/script";
import { compile, type DemoEntry, presets } from "@steamfield/ui/sequencer";
import { useEffect, useMemo, useRef } from "react";

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

/**
 * rr's pacing on the library's terminal preset: 28ms per character (typing capped at
 * 1600ms for a command, 2000ms for a message), 55ms per word, 45ms per output line
 * capped at 2400ms, 900ms per tool call and 500ms after each step. A word and the space
 * after it are two units, so the last word's missing space comes back in `readMs`. A tool
 * call is 400ms running (its dot turns green) plus the 500ms gap.
 */
const TIMING = { ...presets.terminal, readMs: 527.5, toolMs: () => 400 };

function toStep(step: Step): DemoStep {
  switch (step.kind) {
    case "command":
    case "user":
      return step;
    case "agent":
      return { kind: "agent-text", text: step.text };
    case "tool":
      return { kind: "tool-call", name: step.name, args: step.arg };
    case "output":
      return { kind: "output", lines: step.lines };
    case "tool-output":
      return step.more === undefined
        ? { kind: "output", lines: step.lines }
        : { kind: "output", lines: step.lines, more: step.more };
    case "note":
      return { kind: "pause", ms: 500 };
  }
}

/** One library step per rr step, so entry indices are rr's step indices. */
function toScript(steps: readonly Step[]): DemoScript {
  return { steps: steps.map(toStep) };
}

/** Start time of each step, plus the script's total length. */
export function schedule(steps: readonly Step[]): { starts: number[]; total: number } {
  const timeline = compile(toScript(steps), { timing: TIMING });
  return { starts: timeline.steps.map((step) => step.start), total: timeline.duration };
}

const toneClass: Record<Tone, string> = {
  ok: "text-success",
  fail: "text-error",
  warn: "text-waiting",
  muted: "text-muted",
};

function Lines({ lines, prefix }: { lines: readonly Line[]; prefix?: string }) {
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

/** `fresh`: the step starts at this very moment, which rr shows as its prompt alone. */
function renderStep(step: Step, entry: DemoEntry, fresh: boolean) {
  const text = fresh ? "" : (entry.visibleText ?? "");
  const lines = (fresh ? [] : (entry.visibleLines ?? [])) as readonly Line[];
  switch (step.kind) {
    case "command":
      return (
        <div key={entry.key} className="text-ink">
          <span aria-hidden="true" className="text-accent">
            ${" "}
          </span>
          {text}
        </div>
      );
    case "output":
      return (
        <div key={entry.key}>
          <Lines lines={lines} />
        </div>
      );
    case "user":
      return (
        <div
          key={entry.key}
          className="text-ink bg-canvas/60 rounded-control -mx-1 my-1 px-1 whitespace-pre-wrap"
        >
          <span aria-hidden="true" className="text-muted">
            ❯{" "}
          </span>
          {text}
        </div>
      );
    case "agent":
      return (
        <div key={entry.key} className="text-ink my-1 whitespace-pre-wrap">
          <span aria-hidden="true" className="text-ink">
            ●{" "}
          </span>
          {text}
        </div>
      );
    case "tool":
      return (
        <div key={entry.key} className="mt-1">
          <span
            aria-hidden="true"
            className={entry.status === "running" ? "text-waiting" : "text-success"}
          >
            ●{" "}
          </span>
          <span className="text-ink font-bold">{step.name}</span>
          <span className="text-ink-2">({step.arg})</span>
        </div>
      );
    case "tool-output":
      return (
        <div key={entry.key} className="mb-1">
          <Lines lines={lines} prefix="  ⎿  " />
          {entry.more !== undefined && (
            <div className="text-muted">{`     … +${entry.more} lines`}</div>
          )}
        </div>
      );
    case "note":
      return (
        <div key={entry.key} className="text-muted">
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
  const script = useMemo(() => toScript(steps), [steps]);
  const demo = useAgentDemo(script, { timing: TIMING });
  const bodyRef = useRef<HTMLElement>(null);
  const still = !demo.ready || demo.reduced;

  // biome-ignore lint/correctness/useExhaustiveDependencies: follow the newest output while playing
  useEffect(() => {
    const node = bodyRef.current;
    if (node && demo.playing) node.scrollTop = node.scrollHeight;
  }, [demo.view.elapsed, demo.playing]);

  return (
    <div ref={demo.ref} className="term">
      <div className="term-bar justify-between">
        <span className="truncate">{title}</span>
        {!still && (
          <button
            type="button"
            onClick={demo.complete ? demo.controls.replay : demo.controls.toggle}
            className="rounded-control border-line-strong text-ink-2 hover:text-ink shrink-0 border px-2 py-0.5 text-[11px]"
          >
            {demo.complete ? "Replay" : demo.paused ? "Play" : "Pause"}
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
        {demo.view.entries.map((entry) => {
          const step = steps[entry.index];
          const start = demo.timeline.steps[entry.index]?.start ?? 0;
          const fresh = !demo.view.complete && start >= demo.view.elapsed;
          return step ? renderStep(step, entry, fresh) : null;
        })}
      </section>
    </div>
  );
}
