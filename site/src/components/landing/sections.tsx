// Static landing sections. They render to HTML at build time and ship no
// JavaScript; only components listed in site.islands may take a client: directive.
// Each section returns null when its config is empty.
import { type CSSProperties, type ReactNode, useId } from "react";
import type { Link, Row } from "../../config";

export function ReleaseBanner({ announcement }: { announcement: Link | null }) {
  if (!announcement) return null;
  return (
    <p className="bg-panel border-line border-b px-4 py-2 text-center text-sm">
      <a className="text-info font-medium" href={announcement.href}>
        {announcement.label}
      </a>
    </p>
  );
}

/** Plays on load (.hero-in): the headline blurs in, the lede follows. */
export function Hero({ tagline, description }: { tagline: string; description: string }) {
  return (
    <div className="flex max-w-[46rem] flex-col gap-6">
      <h1
        data-motion="blur-in-soft"
        className="hero-in text-ink text-[clamp(2.75rem,6.5vw,4.75rem)] leading-[0.98] font-semibold tracking-[-0.045em] text-balance"
      >
        {tagline}
      </h1>
      <p
        data-motion="lift-md"
        style={{ "--motion-delay": "180ms" } as CSSProperties}
        className="hero-in text-ink-2 max-w-[38rem] text-lg leading-relaxed sm:text-xl"
      >
        {description}
      </p>
    </div>
  );
}

/** Renders `backticked` spans of a plain string as unbreakable inline code. */
export function InlineCode({ text }: { text: string }) {
  return (
    <>
      {text.split(/(`[^`]+`)/).map((part, i) =>
        part.startsWith("`") && part.endsWith("`") ? (
          // biome-ignore lint/suspicious/noArrayIndexKey: fixed copy that never reorders
          <code key={i} className="text-ink font-mono text-[0.9em] whitespace-nowrap">
            {part.slice(1, -1)}
          </code>
        ) : (
          part
        ),
      )}
    </>
  );
}

/**
 * A full-width section. `band` puts it on the panel surface, edge to edge, so
 * sections alternate and each one reads as its own block. The id is the anchor
 * and the heading's label.
 *
 * The section reveals as one sequence when it scrolls into view: the heading at
 * 0ms, the lede at 120ms, and the content from 240ms, which children pick up as
 * the inherited --motion-delay and --stagger-from.
 */
export function Section({
  id,
  title,
  lede,
  band = false,
  track = false,
  children,
}: {
  id: string;
  title: string;
  lede?: ReactNode;
  band?: boolean;
  /** Report a section_viewed event when it scrolls into view (src/scripts/site.ts). */
  track?: boolean;
  children: ReactNode;
}) {
  return (
    <section
      id={id}
      aria-labelledby={`${id}-title`}
      data-track-view={track || undefined}
      data-reveal
      className={`scroll-mt-14 py-20 sm:py-28 ${band ? "band" : ""}`}
    >
      <div className="mx-auto max-w-[1200px] px-4 sm:px-8">
        <div className="mb-10 flex max-w-[44rem] flex-col gap-4 sm:mb-12">
          <h2
            id={`${id}-title`}
            data-motion="lift-lg"
            className="text-ink text-[clamp(1.875rem,3.6vw,2.75rem)] leading-[1.05] font-semibold tracking-[-0.035em] text-balance"
          >
            {title}
          </h2>
          {lede && (
            <div
              data-motion="lift-md"
              style={{ "--motion-delay": "120ms" } as CSSProperties}
              className="text-ink-2 text-lg leading-relaxed"
            >
              {typeof lede === "string" ? <InlineCode text={lede} /> : lede}
            </div>
          )}
        </div>
        <div style={{ "--motion-delay": "240ms", "--stagger-from": "240ms" } as CSSProperties}>
          {children}
        </div>
      </div>
    </section>
  );
}

/** The "why": the same work on one laptop and with rr, side by side. */
export function BeforeAfter({
  before,
  after,
}: {
  before: { title: string; lines: string[] };
  after: { title: string; lines: string[] };
}) {
  if (before.lines.length === 0 && after.lines.length === 0) return null;
  const column = (side: typeof before, ok: boolean) => (
    <div
      data-motion="lift-card"
      className={`rounded-panel border p-6 sm:p-8 ${ok ? "border-line-strong bg-canvas" : "border-line bg-canvas/40"}`}
    >
      <h3 className="text-ink mb-5 flex items-center gap-2.5 text-lg font-semibold">
        <span aria-hidden="true" className={`font-mono ${ok ? "text-success" : "text-error"}`}>
          {ok ? "◉" : "✕"}
        </span>
        {side.title}
      </h3>
      <ul
        data-stagger
        style={
          {
            "--stagger-from": "calc(var(--motion-delay, 0ms) + 200ms)",
            "--stagger-gap": "75ms",
          } as CSSProperties
        }
        className="flex flex-col gap-4"
      >
        {side.lines.map((line) => (
          <li key={line} data-motion="lift-sm" className="text-ink-2 flex gap-3 leading-relaxed">
            <span
              aria-hidden="true"
              className={`mt-[0.45em] h-1.5 w-1.5 shrink-0 rounded-full ${ok ? "bg-success" : "bg-error"}`}
            />
            {line}
          </li>
        ))}
      </ul>
    </div>
  );
  return (
    <div
      data-stagger
      style={{ "--stagger-gap": "140ms" } as CSSProperties}
      className="grid gap-4 lg:grid-cols-2"
    >
      {column(before, false)}
      {column(after, true)}
    </div>
  );
}

export function Transcript({ command, lines }: { command: string; lines: string[] }) {
  return (
    <div data-motion="scale-in-media" className="term">
      <div className="term-bar">
        <span>
          <span aria-hidden="true" className="text-accent">
            ${" "}
          </span>
          {command}
        </span>
      </div>
      <pre
        data-stagger
        style={
          {
            "--stagger-gap": "45ms",
            "--stagger-from": "calc(var(--motion-delay, 0ms) + 150ms)",
          } as CSSProperties
        }
        className="px-3 py-3 text-[12px] whitespace-pre-wrap"
      >
        {lines.map((line, i) => (
          <span
            // biome-ignore lint/suspicious/noArrayIndexKey: a fixed recording that never reorders
            key={i}
            data-motion="lift-sm"
            style={{ "--stagger": i } as CSSProperties}
            className={`block ${/^\s*◉/.test(line) ? "text-success" : line.startsWith("Parallel") ? "text-ink font-bold" : "text-ink-2"}`}
          >
            {line || " "}
          </span>
        ))}
      </pre>
    </div>
  );
}

export type TaskTiming = { name: string; host: string; seconds: number };

/**
 * The speedup as the work itself: the three tasks end to end on one machine, then
 * the same tasks side by side on three hosts. Bar length is time.
 */
export function TaskChart({ tasks }: { tasks: TaskTiming[] }) {
  const captionId = useId();
  if (tasks.length === 0) return null;
  const sequential = tasks.reduce((sum, task) => sum + task.seconds, 0);
  const parallel = Math.max(...tasks.map((task) => task.seconds));
  const pct = (seconds: number) => `${(seconds / sequential) * 100}%`;
  const tones = ["bg-accent", "bg-sync", "bg-info"];
  return (
    <figure aria-labelledby={captionId} className="flex flex-col gap-8">
      <figcaption id={captionId} className="sr-only">
        On one machine the tasks run one after another: {sequential.toFixed(1)} seconds. On three
        hosts they run at once and the slowest sets the time: {parallel} seconds.
      </figcaption>
      <div aria-hidden="true">
        <div className="mb-2 flex items-baseline justify-between text-sm">
          <span className="text-ink-2">One machine, one task after another</span>
          <span className="text-ink font-mono text-base">{sequential.toFixed(1)}s</span>
        </div>
        <div className="rounded-control grow-x flex h-9 gap-px overflow-hidden">
          {tasks.map((task, i) => (
            <div
              key={task.name}
              className={`${tones[i % tones.length]} text-canvas flex items-center px-2 font-mono text-[11px] font-semibold whitespace-nowrap`}
              style={{ width: pct(task.seconds) }}
            >
              {task.seconds >= 10 ? task.name : ""}
            </div>
          ))}
        </div>
      </div>
      <div aria-hidden="true">
        <div className="mb-2 flex items-baseline justify-between text-sm">
          <span className="text-ink-2">Three hosts, at the same time</span>
          <span className="text-success font-mono text-base">{parallel}s</span>
        </div>
        <div className="flex flex-col gap-1.5">
          {tasks.map((task, i) => (
            <div key={task.name} className="flex items-center gap-3">
              <div className="bg-canvas rounded-control relative h-7 flex-1 overflow-hidden">
                <div
                  className={`${tones[i % tones.length]} text-canvas grow-x absolute inset-y-0 left-0 flex items-center px-2 font-mono text-[11px] font-semibold whitespace-nowrap`}
                  style={{ width: pct(task.seconds), "--stagger": i + 1 } as CSSProperties}
                >
                  {task.seconds >= 10 ? task.name : ""}
                </div>
              </div>
              <span className="text-muted w-20 shrink-0 font-mono text-[11px]">{task.host}</span>
            </div>
          ))}
        </div>
      </div>
    </figure>
  );
}

export function DataTable({
  caption,
  head,
  rows,
  mono = 0,
}: {
  caption: string;
  head: string[];
  rows: Row[];
  /** Number of leading columns set in mono. */
  mono?: number;
}) {
  if (rows.length === 0) return null;
  return (
    <div data-motion="lift-md" className="rounded-panel border-line overflow-x-auto border">
      <table className="w-full border-collapse text-left text-sm">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr className="bg-panel">
            {head.map((cell) => (
              <th key={cell} scope="col" className="text-muted px-4 py-2.5 text-xs font-medium">
                {cell}
              </th>
            ))}
          </tr>
        </thead>
        <tbody
          data-stagger
          style={
            {
              "--stagger-gap": "60ms",
              "--stagger-from": "calc(var(--motion-delay, 0ms) + 150ms)",
            } as CSSProperties
          }
        >
          {rows.map((row, index) => (
            <tr
              key={row.join("|")}
              data-motion="lift-sm"
              style={{ "--stagger": index } as CSSProperties}
              className="border-line border-t"
            >
              {row.map((cell, i) =>
                i === 0 ? (
                  <th
                    key={cell}
                    scope="row"
                    className={`text-ink px-4 py-3 align-top font-medium whitespace-nowrap ${i < mono ? "font-mono text-[13px]" : ""}`}
                  >
                    {cell}
                  </th>
                ) : (
                  <td
                    key={cell}
                    className={`text-ink-2 px-4 py-3 align-top ${i < mono ? "font-mono text-[13px]" : ""}`}
                  >
                    {cell}
                  </td>
                ),
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function Requirements({ items }: { items: string[] }) {
  if (items.length === 0) return null;
  return (
    <div>
      <h3 className="text-ink mb-3 font-semibold">What you need</h3>
      <ul
        data-stagger
        style={
          {
            "--stagger-gap": "60ms",
            "--stagger-from": "calc(var(--motion-delay, 0ms) + 150ms)",
          } as CSSProperties
        }
        className="text-ink-2 flex flex-col gap-2 text-sm"
      >
        {items.map((item) => (
          <li key={item} data-motion="lift-sm" className="flex gap-2">
            <span aria-hidden="true" className="text-info font-mono">
              ◇
            </span>
            {item}
          </li>
        ))}
      </ul>
    </div>
  );
}

export function CommandList({ title, commands }: { title: string; commands: string[] }) {
  if (commands.length === 0) return null;
  return (
    <div className="term">
      <div className="term-bar">{title}</div>
      <pre
        data-stagger
        style={
          {
            "--stagger-gap": "60ms",
            "--stagger-from": "calc(var(--motion-delay, 0ms) + 150ms)",
          } as CSSProperties
        }
        className="text-ink px-3 py-3 text-[12px] whitespace-pre-wrap"
      >
        {commands.map((command) => (
          <span key={command} data-motion="lift-sm" className="block">
            {command}
          </span>
        ))}
      </pre>
    </div>
  );
}
