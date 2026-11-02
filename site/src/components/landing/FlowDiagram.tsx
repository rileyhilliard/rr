// "How it works": agents on one laptop call rr, and every call is its own rr
// process. Each process tries the host locks top to bottom, waits on the laptop
// when every host is taken, and retries on its own timer; a parallel task locks a
// host per subtask. The timeline comes from src/lib/flow.ts. Before hydration and
// under reduced motion it holds the moment one parallel run has every host and two
// other runs are waiting.
import { type ReactNode, useEffect, useMemo } from "react";
import {
  type ChipView,
  FLOW_HOSTS,
  FLOW_RUNS,
  type FlowRun,
  flowSteps,
  type HostView,
  type Scene,
  type Segment,
  sceneAt,
  simulate,
} from "../../lib/flow";
import { usePlayback } from "../../lib/use-playback";
import { InlineCode } from "./sections";

const HOLD_MS = 3000;
/** Agent 1's subtasks hold every host and agents 2 and 3 wait. */
const STILL_AT = 9000;
const SUMMARY =
  "Four coding agents on one laptop call rr. Each call is its own rr process, with no server in between. Agent 1 runs a parallel task, and its rr locks all three hosts, one subtask each. Agents 2 and 3 find every host locked and wait on the laptop, retrying every two seconds. When agent 1's run ends and the locks release, agent 2 retries first and takes the first host; agent 3 finds that taken and takes the second. Agent 4 arrives later and takes the free third host without waiting.";

type Rect = { x: number; y: number; w: number; h: number };
type Point = { x: number; y: number };
type Port = Point & { dx: number; dy: number };

type Geometry = {
  width: number;
  height: number;
  compact: boolean;
  laptop: Rect;
  agent: (i: number) => Rect;
  /** The rr process's box; `slot` 1 is the waiting column. */
  chip: (i: number, slot: number) => Rect;
  host: (i: number) => Rect;
  chipPort: (box: Rect) => Port;
  hostPort: (i: number) => Port;
  waiting?: Rect;
  labels: { x: number; y: number; text: string; anchor?: "middle" | "end"; className?: string }[];
  font: { name: number; code: number };
};

const ROW = (y0: number, dy: number, h: number) => (i: number) => ({ y: y0 + i * dy, h });

const wideRow = ROW(84, 110, 94);
const WIDE: Geometry = {
  width: 1000,
  height: 540,
  compact: false,
  laptop: { x: 12, y: 40, w: 560, h: 488 },
  agent: (i) => ({ x: 28, w: 200, ...wideRow(i) }),
  chip: (i, slot) => {
    const row = wideRow(i);
    return { x: 404 - slot * 163, y: row.y + 18, w: 150, h: 58 };
  },
  host: (i) => ({ x: 720, y: 84 + i * 148, w: 268, h: 128 }),
  chipPort: (box) => ({ x: box.x + box.w, y: box.y + box.h / 2, dx: 1, dy: 0 }),
  hostPort: (i) => ({ x: 720, y: 84 + i * 148 + 64, dx: -1, dy: 0 }),
  waiting: { x: 236, y: 76, w: 160, h: 440 },
  labels: [
    { x: 12, y: 26, text: "Your laptop: one rr process per call, no server" },
    { x: 720, y: 26, text: "Your hosts, tried top to bottom" },
    { x: 28, y: 66, text: "Agents" },
    { x: 316, y: 66, text: "Waiting for a host", anchor: "middle", className: "fill-waiting" },
    { x: 479, y: 66, text: "rr, working", anchor: "middle" },
    { x: 646, y: 26, text: "SSH", anchor: "middle" },
  ],
  font: { name: 13, code: 10.5 },
};

const tallRow = ROW(298, 84, 72);
const TALL: Geometry = {
  width: 360,
  height: 640,
  compact: true,
  laptop: { x: 4, y: 276, w: 352, h: 356 },
  agent: (i) => ({ x: 12, w: 160, ...tallRow(i) }),
  chip: (i) => {
    const row = tallRow(i);
    return { x: 182, y: row.y + 9, w: 166, h: 54 };
  },
  host: (i) => ({ x: 4 + i * 120, y: 26, w: 112, h: 108 }),
  chipPort: (box) => ({ x: box.x + box.w / 2, y: box.y, dx: 0, dy: -1 }),
  hostPort: (i) => ({ x: 4 + i * 120 + 56, y: 134, dx: 0, dy: 1 }),
  labels: [
    { x: 4, y: 16, text: "Your hosts, tried left to right" },
    { x: 4, y: 266, text: "Your laptop: one rr process per call" },
  ],
  font: { name: 12, code: 10 },
};

/** A cubic from one port to another, and a function for points along it. */
function curve(a: Port, b: Port) {
  const reach = Math.max(Math.abs(b.x - a.x), Math.abs(b.y - a.y)) * 0.5;
  const c1 = { x: a.x + a.dx * reach, y: a.y + a.dy * reach };
  const c2 = { x: b.x + b.dx * reach, y: b.y + b.dy * reach };
  const at = (u: number): Point => {
    const v = 1 - u;
    return {
      x: v * v * v * a.x + 3 * v * v * u * c1.x + 3 * v * u * u * c2.x + u * u * u * b.x,
      y: v * v * v * a.y + 3 * v * v * u * c1.y + 3 * v * u * u * c2.y + u * u * u * b.y,
    };
  };
  return { d: `M${a.x} ${a.y}C${c1.x} ${c1.y} ${c2.x} ${c2.y} ${b.x} ${b.y}`, at };
}

function Text({
  x,
  y,
  size,
  className,
  anchor,
  opacity,
  children,
}: {
  x: number;
  y: number;
  size: number;
  className: string;
  anchor?: "middle" | "end";
  opacity?: number;
  children: ReactNode;
}) {
  return (
    <text x={x} y={y} fontSize={size} textAnchor={anchor} opacity={opacity} className={className}>
      {children}
    </text>
  );
}

/** A label on a line, on a canvas chip so the line doesn't run through it. */
function Pill({
  at,
  text,
  className,
  size,
}: {
  at: Point;
  text: string;
  className: string;
  size: number;
}) {
  const w = text.length * size * 0.66 + 10;
  return (
    <g>
      <rect
        x={at.x - w / 2}
        y={at.y - size * 0.95}
        width={w}
        height={size * 1.7}
        rx={2}
        className="fill-canvas stroke-line"
      />
      <Text
        x={at.x}
        y={at.y + size * 0.35}
        size={size}
        anchor="middle"
        className={`font-mono ${className}`}
      >
        {text}
      </Text>
    </g>
  );
}

function Padlock({ x, y, locked }: { x: number; y: number; locked: boolean }) {
  // 12x14: shackle closed into the body when locked, swung open when free.
  return (
    <g
      transform={`translate(${x} ${y})`}
      className={locked ? "stroke-ink" : "stroke-muted"}
      fill="none"
    >
      <path d={locked ? "M3 6V4a3 3 0 0 1 6 0v2" : "M3 6V4a3 3 0 0 1 6 0"} strokeWidth={1.4} />
      <rect
        x={1}
        y={6}
        width={10}
        height={8}
        rx={1.5}
        className={locked ? "fill-ink" : "fill-none"}
        strokeWidth={1.4}
      />
    </g>
  );
}

function Links({ g, scene, t, slots }: { g: Geometry; scene: Scene; t: number; slots: number[] }) {
  // One label per run and kind: a parallel run's three lines share it.
  const labelled = new Set<string>();
  return (
    <g fill="none">
      {scene.active.map(({ segment, at }) => {
        const kind = `${segment.run}-${segment.kind.startsWith("probe") ? "probe" : segment.kind}`;
        const showLabel = !labelled.has(kind);
        labelled.add(kind);
        return (
          <Link
            key={`${segment.run}-${segment.host}-${segment.kind}-${segment.start}`}
            g={g}
            t={t}
            segment={segment}
            at={at}
            slot={slots[segment.run] ?? 0}
            label={showLabel}
          />
        );
      })}
    </g>
  );
}

function Link({
  g,
  t,
  segment,
  at,
  slot,
  label,
}: {
  g: Geometry;
  t: number;
  segment: Segment;
  at: number;
  slot: number;
  label: boolean;
}) {
  const { d, at: point } = curve(g.chipPort(g.chip(segment.run, slot)), g.hostPort(segment.host));
  const size = g.compact ? 9 : 10;
  const ok = FLOW_RUNS[segment.run]?.result.ok ?? true;
  if (segment.kind === "probe-ok" || segment.kind === "probe-locked") {
    // A lock attempt draws out from rr to the host; its head is a padlock.
    const head = point(at);
    return (
      <g>
        <path
          d={d}
          pathLength={1}
          strokeDasharray="1 1"
          strokeDashoffset={1 - at}
          className="stroke-ink-2"
          strokeWidth={1.5}
        />
        <Padlock x={head.x - 6} y={head.y - 8} locked={false} />
        {label && at > 0.15 && (
          <Pill at={point(Math.min(at, 0.5) * 0.6)} text="lock?" className="fill-ink" size={size} />
        )}
      </g>
    );
  }
  if (segment.kind === "result") {
    const dot = point(1 - at);
    return (
      <g>
        <path
          d={d}
          className={ok ? "stroke-success" : "stroke-error"}
          strokeWidth={1.5}
          opacity={0.6 * (1 - at)}
        />
        <circle cx={dot.x} cy={dot.y} r={5} className={ok ? "fill-success" : "fill-error"} />
        {label && (
          <Pill
            at={{ x: dot.x, y: dot.y - 16 }}
            text={ok ? "exit 0" : "exit 1"}
            className={ok ? "fill-success" : "fill-error"}
            size={size}
          />
        )}
      </g>
    );
  }
  const sync = segment.kind === "sync";
  const period = sync ? 700 : 1100;
  const count = sync ? 4 : 3;
  return (
    <g>
      <path
        d={d}
        className={sync ? "stroke-sync" : "stroke-info"}
        strokeWidth={1.5}
        opacity={0.4}
      />
      {Array.from({ length: count }, (_, k) => {
        const u = (t / period + k / count) % 1;
        const dot = point(sync ? u : 1 - u);
        return (
          <circle
            // biome-ignore lint/suspicious/noArrayIndexKey: a fixed number of dots that never reorder
            key={k}
            cx={dot.x}
            cy={dot.y}
            r={2.5}
            className={sync ? "fill-sync" : "fill-info"}
          />
        );
      })}
      {label && (
        <Pill
          at={point(0.5)}
          text={sync ? "rsync" : "output"}
          className={sync ? "fill-sync" : "fill-info"}
          size={size}
        />
      )}
    </g>
  );
}

/** "✕ locked" or "✓ yours" at the host end of a lock attempt that just landed. */
function Tags({ g, scene }: { g: Geometry; scene: Scene }) {
  const size = g.compact ? 9 : 10.5;
  return (
    <g>
      {scene.tags.map((tag) => {
        const port = g.hostPort(tag.host);
        const fade = Math.min(1, (tag.ms - tag.age) / 200);
        const text = tag.ok ? "✓ yours" : "✕ locked";
        const at = g.compact
          ? { x: port.x, y: port.y + 16 }
          : { x: port.x - text.length * size * 0.33 - 12, y: port.y - 18 };
        return (
          <g key={`${tag.run}-${tag.host}-${tag.ok}`} opacity={fade}>
            <Pill
              at={at}
              text={text}
              className={tag.ok ? "fill-success" : "fill-error"}
              size={size}
            />
          </g>
        );
      })}
    </g>
  );
}

function agentLine(run: FlowRun, compact: boolean) {
  if (!compact) return run.command;
  return run.command.replace(/^rr run "(.*)"$/, "$1");
}

function Agent({
  g,
  i,
  chip,
  result,
}: {
  g: Geometry;
  i: number;
  chip: ChipView;
  result?: FlowRun["result"];
}) {
  const run = FLOW_RUNS[i];
  if (!run) return null;
  const box = g.agent(i);
  const pad = 12;
  const status = result
    ? {
        text: `${result.ok ? "◉" : "✕"} ${result.text}`,
        className: result.ok ? "fill-success" : "fill-error",
      }
    : chip.visible
      ? { text: "◐ waiting on rr", className: "fill-muted" }
      : { text: "◌ writing code", className: "fill-muted" };
  const line = g.compact ? 19 : 22;
  return (
    <g data-agent={i}>
      <rect
        x={box.x}
        y={box.y}
        width={box.w}
        height={box.h}
        rx={4}
        className={`fill-canvas ${result ? (result.ok ? "stroke-success" : "stroke-error") : "stroke-line"}`}
      />
      <Text
        x={box.x + pad}
        y={box.y + pad + 11}
        size={g.font.name}
        className="fill-ink font-sans font-semibold"
      >
        {run.agent}
      </Text>
      <Text
        x={box.x + pad}
        y={box.y + pad + 11 + line}
        size={g.font.code}
        className="fill-ink-2 font-mono"
      >
        {agentLine(run, g.compact)}
      </Text>
      <Text
        x={box.x + pad}
        y={box.y + pad + 11 + line * 2}
        size={g.font.code}
        className={`font-mono ${status.className}`}
      >
        {status.text}
      </Text>
    </g>
  );
}

function chipStatus(i: number, chip: ChipView, scene: Scene) {
  const run = FLOW_RUNS[i];
  const held = scene.hosts.flatMap((host, h) => (host.holder === i ? [{ host, h }] : []));
  if (chip.state === "done") {
    return run?.result.ok
      ? { text: "exit 0", className: "fill-success" }
      : { text: "exit 1", className: "fill-error" };
  }
  if (chip.state === "waiting" && chip.retryIn !== undefined) {
    return { text: `retry in ${(chip.retryIn / 1000).toFixed(1)}s`, className: "fill-waiting" };
  }
  const probes = scene.active.filter(
    (a) => a.segment.run === i && a.segment.kind.startsWith("probe"),
  );
  const probe = probes[0];
  if (probes.length > 1) return { text: `lock ${probes.length} hosts?`, className: "fill-ink" };
  if (probe) return { text: `lock ${FLOW_HOSTS[probe.segment.host]}?`, className: "fill-ink" };
  if (held.length > 1) {
    const syncing = held.some(({ host }) => host.phase === "sync");
    return {
      text: `${held.length} subtasks ${syncing ? "syncing" : "running"}`,
      className: syncing ? "fill-sync" : "fill-info",
    };
  }
  const first = held[0];
  if (first) {
    const name = FLOW_HOSTS[first.h];
    return first.host.phase === "sync"
      ? { text: `rsync to ${name}`, className: "fill-sync" }
      : { text: `running on ${name}`, className: "fill-info" };
  }
  return { text: "collecting results", className: "fill-ink-2" };
}

function Chip({
  g,
  i,
  chip,
  scene,
  t,
}: {
  g: Geometry;
  i: number;
  chip: ChipView;
  scene: Scene;
  t: number;
}) {
  if (!chip.visible) return null;
  const run = FLOW_RUNS[i];
  const box = g.chip(i, chip.slot);
  const agent = g.agent(i);
  const status = chipStatus(i, chip, scene);
  const waiting = chip.state === "waiting";
  const enter = Math.min(1, (t - (run?.arrive ?? 0)) / 300);
  const stroke = waiting
    ? "stroke-waiting"
    : chip.state === "done"
      ? "stroke-line-strong"
      : "stroke-accent";
  const ring = waiting && chip.retryIn !== undefined ? 1 - chip.retryIn / 2000 : undefined;
  const mid = agent.y + agent.h / 2;
  return (
    <g data-chip={i} data-state={chip.state} opacity={enter}>
      {/* The agent's call: from the agent to its own rr process. */}
      {!g.compact && (
        <path
          d={`M${agent.x + agent.w} ${mid}H${box.x}`}
          className="stroke-accent"
          strokeWidth={1}
          opacity={0.5}
        />
      )}
      <rect
        x={box.x}
        y={box.y}
        width={box.w}
        height={box.h}
        rx={4}
        className={`fill-canvas ${stroke}`}
        strokeWidth={1.25}
      />
      <Text x={box.x + 10} y={box.y + 20} size={12} className="fill-accent font-mono font-bold">
        rr
      </Text>
      <Text x={box.x + 30} y={box.y + 20} size={g.font.code} className="fill-muted font-mono">
        {`pid ${48210 + i * 37}`}
      </Text>
      {ring !== undefined && (
        <g transform={`translate(${box.x + box.w - 16} ${box.y + 16})`}>
          <circle r={7} fill="none" className="stroke-line" strokeWidth={2} />
          <circle
            r={7}
            fill="none"
            className="stroke-waiting"
            strokeWidth={2}
            pathLength={1}
            strokeDasharray={`${ring} 1`}
            transform="rotate(-90)"
          />
        </g>
      )}
      <Text
        x={box.x + 10}
        y={box.y + 42}
        size={g.font.code}
        className={`font-mono ${status.className}`}
      >
        {status.text}
      </Text>
    </g>
  );
}

function Host({ g, i, view }: { g: Geometry; i: number; view: HostView }) {
  const box = g.host(i);
  const pad = g.compact ? 10 : 14;
  const holder = view.holder === undefined ? undefined : FLOW_RUNS[view.holder];
  const barY = box.y + box.h - pad - 5;
  const barW = box.w - pad * 2;
  const phase =
    view.phase === "sync"
      ? { text: "rsync", className: "fill-sync" }
      : view.phase === "test"
        ? { text: `tests ${Math.round(view.progress * 100)}%`, className: "fill-info" }
        : view.phase === "hold"
          ? { text: g.compact ? "done, held" : "done, held for the run", className: "fill-muted" }
          : { text: "idle", className: "fill-muted" };
  const who = holder
    ? g.compact
      ? holder.agent
      : `${holder.agent}${view.task ? ` · ${view.task}` : ""}`
    : "free";
  return (
    <g data-host={i} data-holder={view.holder ?? ""}>
      <rect
        x={box.x}
        y={box.y}
        width={box.w}
        height={box.h}
        rx={6}
        className={`fill-panel ${holder ? "stroke-line-strong" : "stroke-line"}`}
      />
      <Text
        x={box.x + pad}
        y={box.y + pad + 12}
        size={g.font.name + 1}
        className="fill-ink font-mono font-semibold"
      >
        {FLOW_HOSTS[i]}
      </Text>
      {!g.compact && <Padlock x={box.x + box.w - pad - 12} y={box.y + pad} locked={!!holder} />}
      <Text
        x={box.x + pad}
        y={box.y + pad + (g.compact ? 32 : 38)}
        size={g.font.code}
        className={`font-mono ${holder ? "fill-ink" : "fill-muted"}`}
      >
        {g.compact ? `${holder ? "◆" : "◇"} ${who}` : who}
      </Text>
      {g.compact && holder && view.task && (
        <Text
          x={box.x + pad}
          y={box.y + pad + 47}
          size={g.font.code}
          className="fill-ink-2 font-mono"
        >
          {view.task}
        </Text>
      )}
      <Text
        x={box.x + pad}
        y={barY - 8}
        size={g.font.code}
        className={`font-mono ${phase.className}`}
      >
        {phase.text}
      </Text>
      <rect x={box.x + pad} y={barY} width={barW} height={4} rx={1} className="fill-line" />
      {(view.phase === "sync" || view.phase === "test") && (
        <rect
          x={box.x + pad}
          y={barY}
          width={barW * Math.min(1, view.progress)}
          height={4}
          rx={1}
          className={view.phase === "sync" ? "fill-sync" : "fill-info"}
        />
      )}
      {view.phase === "hold" && (
        <rect x={box.x + pad} y={barY} width={barW} height={4} rx={1} className="fill-muted" />
      )}
    </g>
  );
}

function Diagram({
  g,
  scene,
  t,
  className,
}: {
  g: Geometry;
  scene: Scene;
  t: number;
  className: string;
}) {
  const slots = g.compact ? scene.chips.map(() => 0) : scene.chips.map((chip) => chip.slot);
  const chips = scene.chips.map((chip, i) => ({ ...chip, slot: slots[i] ?? 0 }));
  return (
    <svg
      viewBox={`0 0 ${g.width} ${g.height}`}
      role="img"
      className={`block h-auto w-full ${className}`}
    >
      <title>{SUMMARY}</title>
      {g.labels.map((label) => (
        <Text
          key={label.text}
          x={label.x}
          y={label.y}
          size={g.compact ? 11 : 12}
          anchor={label.anchor}
          className={`font-sans ${label.className ?? "fill-muted"}`}
        >
          {label.text}
        </Text>
      ))}
      <rect
        x={g.laptop.x}
        y={g.laptop.y}
        width={g.laptop.w}
        height={g.laptop.h}
        rx={6}
        className="fill-panel stroke-line"
      />
      {g.waiting && (
        <rect
          x={g.waiting.x}
          y={g.waiting.y}
          width={g.waiting.w}
          height={g.waiting.h}
          rx={6}
          fill="none"
          className="stroke-waiting"
          strokeDasharray="4 4"
          opacity={0.45}
        />
      )}
      {scene.hosts.map((view, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: hosts are fixed and never reorder
        <Host key={i} g={g} i={i} view={view} />
      ))}
      <Links g={g} scene={scene} t={t} slots={slots} />
      {scene.chips.map((_, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: agents are fixed and never reorder
        <Agent key={i} g={g} i={i} chip={chips[i] as ChipView} result={scene.results[i]} />
      ))}
      {chips.map((chip, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: agents are fixed and never reorder
        <Chip key={i} g={g} i={i} chip={chip} scene={scene} t={t} />
      ))}
      <Tags g={g} scene={scene} />
    </svg>
  );
}

export function FlowDiagram() {
  const segments = useMemo(() => simulate(FLOW_RUNS, FLOW_HOSTS.length), []);
  const steps = useMemo(() => flowSteps(segments, FLOW_RUNS, FLOW_HOSTS), [segments]);
  const end = Math.max(...segments.map((s) => s.end)) + 900;
  const playback = usePlayback(end + HOLD_MS, 1, true);

  // It loops: after the hold at the end, start over.
  const { complete, still, replay } = playback;
  useEffect(() => {
    if (complete && !still) replay();
  }, [complete, still, replay]);

  const t = playback.still ? STILL_AT : Math.min(playback.elapsed, end);
  const scene = sceneAt(segments, FLOW_RUNS, FLOW_HOSTS.length, t);
  const current = steps.findLastIndex((step) => t >= step.at);
  const live = steps[Math.max(0, current)];

  return (
    <div ref={playback.ref} className="flex flex-col gap-6">
      <figure className="border-line bg-canvas rounded-panel overflow-hidden border">
        <div className="p-3 sm:p-6">
          <Diagram g={WIDE} scene={scene} t={t} className="hidden sm:block" />
          <Diagram g={TALL} scene={scene} t={t} className="sm:hidden" />
        </div>
        {!playback.still && live && (
          // The beat being shown, next to the motion. Not live-announced: it changes every few seconds.
          <p className="border-line text-ink min-h-[4.5rem] border-t px-3 py-3 text-[15px] leading-relaxed sm:px-6 sm:text-base">
            <InlineCode text={live.text} />
          </p>
        )}
        <figcaption className="border-line text-muted flex items-center justify-between gap-3 border-t px-3 py-2 text-xs sm:px-6">
          <span>Simulated from rr's lock rules, with each run shortened to seconds.</span>
          {!playback.still && (
            <button
              type="button"
              onClick={playback.toggle}
              className="rounded-control border-line-strong text-ink-2 hover:text-ink shrink-0 border px-2 py-0.5 font-mono text-[11px]"
            >
              {playback.paused ? "Play" : "Pause"}
            </button>
          )}
        </figcaption>
      </figure>
      {playback.still ? (
        <ol className="text-ink-2 grid gap-x-8 gap-y-4 text-sm leading-relaxed sm:grid-cols-2 lg:grid-cols-3">
          {steps.map((step) => (
            <li key={step.at}>
              <span className="text-ink block font-medium">{step.title}</span>
              <InlineCode text={step.text} />
            </li>
          ))}
        </ol>
      ) : (
        <ol className="grid grid-cols-5 gap-2 sm:gap-4">
          {steps.map((step, i) => {
            const next = steps[i + 1]?.at ?? end;
            const fill =
              i < current ? 1 : i === current ? Math.min(1, (t - step.at) / (next - step.at)) : 0;
            const active = i === current;
            return (
              <li key={step.at}>
                <button
                  type="button"
                  onClick={() => playback.seek(step.at)}
                  aria-current={active ? "step" : undefined}
                  className={`flex w-full flex-col gap-2 text-left text-xs leading-snug transition-colors duration-300 sm:text-sm ${
                    active ? "text-ink" : i < current ? "text-ink-2" : "text-muted"
                  } hover:text-ink`}
                >
                  <span aria-hidden="true" className="bg-line relative block h-0.5 overflow-hidden">
                    <span
                      className="bg-accent absolute inset-y-0 left-0"
                      style={{ width: `${fill * 100}%` }}
                    />
                  </span>
                  <span className="sr-only sm:not-sr-only">{step.title}</span>
                </button>
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}
