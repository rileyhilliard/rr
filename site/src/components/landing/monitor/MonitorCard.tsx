// One host card from `rr monitor`, laid out like renderCard in
// internal/monitor/card.go: status line, the running command, then CPU, GPU,
// latency and RAM graphs, then NET and DISK lines.
import type { ReactNode } from "react";
import {
  formatLockDuration,
  formatPercent,
  formatRate,
  GLYPHS,
  gradientBar,
  type LatencyBand,
  latencyBand,
  RUNNING_FRAMES,
  SPINNER_COLORS,
  severity,
  smooth,
  sparkline,
  THRESHOLDS,
  type Thresholds,
  truncate,
} from "../../../lib/monitor/render";
import type { HostFrame } from "../../../lib/monitor/timeline";
import { BrailleDots, severityClass, UsageBar } from "./Dots";

export type CardHistory = { cpu: number[]; ram: number[]; gpu: number[]; latency: number[] };

type Props = {
  host: string;
  frame: HostFrame;
  history: CardHistory;
  /** Characters across the card's content area; sets graph width like the TUI. */
  cols: number;
  /** Spinner frame index; it ticks on its own clock, as in the TUI. */
  spin: number;
  firing: boolean;
};

const textSeverity = { healthy: "text-info", warning: "text-sync", critical: "text-accent" };
const latencyText: Record<LatencyBand, string> = {
  fast: "text-info",
  normal: "text-lat-normal",
  slow: "text-lat-slow",
  degraded: "text-accent",
};
const latencyStroke: Record<LatencyBand, string> = {
  fast: "stroke-info",
  normal: "stroke-lat-normal",
  slow: "stroke-lat-slow",
  degraded: "stroke-accent",
};

function Line({ left, right }: { left: ReactNode; right?: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-2 whitespace-pre">
      <span className="min-w-0 truncate">{left}</span>
      {right !== undefined && <span className="shrink-0">{right}</span>}
    </div>
  );
}

function Divider() {
  return <div aria-hidden="true" className="border-line my-1.5 border-t" />;
}

const GRAPH_ROWS = 2;

function Graph({
  label,
  values,
  cols,
  thresholds,
}: {
  label: string;
  values: number[];
  cols: number;
  thresholds: Thresholds;
}) {
  const latest = values.at(-1) ?? 0;
  if (values.length < 2) {
    const bar = gradientBar(cols, latest);
    const filled = bar.indexOf("▱") === -1 ? cols : bar.indexOf("▱");
    return (
      <UsageBar
        cells={cols}
        filled={filled}
        cellClass={(i) => severityClass[severity(((i + 1) / cols) * 100, thresholds)]}
      />
    );
  }
  const plot = sparkline(values, cols, GRAPH_ROWS);
  return (
    <BrailleDots
      rows={plot.rows}
      label={`${label} history, now ${latest.toFixed(0)}%`}
      columnClass={(col) => severityClass[severity(plot.columnMax[col] ?? 0, thresholds)]}
    />
  );
}

export function MonitorCard({ host, frame, history, cols, spin, firing }: Props) {
  const graphCols = Math.max(10, cols);
  const spinnerColor = SPINNER_COLORS[spin % SPINNER_COLORS.length];

  let indicator: ReactNode;
  let statusText: ReactNode;
  if (frame.status === "running") {
    indicator = (
      <span style={{ color: spinnerColor }}>{RUNNING_FRAMES[spin % RUNNING_FRAMES.length]}</span>
    );
    statusText = <span className="text-sync"> - running {formatLockDuration(frame.heldMs)}</span>;
  } else if (frame.status === "idle") {
    indicator = <span className="text-info">{GLYPHS.idle}</span>;
    statusText = <span className="text-muted"> - idle</span>;
  } else {
    indicator = <span className="text-accent">{GLYPHS.unreachable}</span>;
    statusText = <span className="text-muted"> - offline</span>;
  }

  // Like renderHostLineWithSSH: the alias shows only when the status line leaves room.
  const statusLength =
    frame.status === "running"
      ? ` - running ${formatLockDuration(frame.heldMs)}`.length
      : frame.status === "idle"
        ? " - idle".length
        : " - offline".length;
  const showAlias =
    frame.status !== "offline" && 2 + host.length + statusLength + 2 + frame.alias.length <= cols;

  const lat = latencyBand(frame.latency);
  const latencyHistory = smooth(history.latency, 5);
  const latencyPlot = sparkline(latencyHistory, graphCols, GRAPH_ROWS, true);

  return (
    <article
      aria-label={`${host}: ${frame.status}${frame.command ? `, running ${frame.command}` : ""}`}
      data-status={frame.status}
      data-firing={firing || undefined}
      className={`bg-panel rounded-panel border p-3 font-mono text-[12px] leading-[1.6] transition-colors duration-150 ${
        firing ? "border-accent" : "border-line"
      }`}
    >
      <Line
        left={
          <>
            <span aria-hidden="true">{indicator}</span>{" "}
            <span className="text-ink font-bold">{host}</span>
            {statusText}
          </>
        }
        right={showAlias ? <span className="text-muted">{frame.alias}</span> : undefined}
      />
      {frame.status === "running" && frame.command && (
        <div className="text-muted truncate whitespace-pre" title={frame.command}>
          {`  > ${truncate(frame.command, Math.max(10, cols - 4))}`}
        </div>
      )}
      {frame.status === "offline" ? (
        <>
          <Divider />
          <div className="text-accent"> Unreachable</div>
        </>
      ) : (
        <>
          <Divider />
          <Line
            left={<span className="text-ink-2">CPU</span>}
            right={
              <>
                <span className={textSeverity[severity(frame.cpu, THRESHOLDS.cpu)]}>
                  {formatPercent(frame.cpu)}
                </span>{" "}
                <span className="text-ink-2">1m:{frame.load1.toFixed(1)}</span>
              </>
            }
          />
          <Graph label="CPU" values={history.cpu} cols={graphCols} thresholds={THRESHOLDS.cpu} />
          {frame.gpu !== null && (
            <>
              <Divider />
              <Line
                left={<span className="text-ink-2">GPU</span>}
                right={
                  <span className={textSeverity[severity(frame.gpu, THRESHOLDS.gpu)]}>
                    {formatPercent(frame.gpu)}
                  </span>
                }
              />
              <Graph
                label="GPU"
                values={history.gpu}
                cols={graphCols}
                thresholds={THRESHOLDS.gpu}
              />
            </>
          )}
          {frame.latency > 0 && (
            <>
              <Divider />
              <Line
                left={<span className="text-ink-2">LAT</span>}
                right={
                  <>
                    <span
                      className={latencyText[lat]}
                    >{`${frame.latency.toFixed(0).padStart(3)}ms`}</span>{" "}
                    <span className="text-ink-2">{lat}</span>
                  </>
                }
              />
              {latencyHistory.length > 1 && (
                <BrailleDots
                  rows={latencyPlot.rows}
                  label={`Latency history, now ${frame.latency.toFixed(0)} ms`}
                  columnClass={(col) => latencyStroke[latencyBand(latencyPlot.columnMax[col] ?? 0)]}
                />
              )}
            </>
          )}
          <Divider />
          <Line
            left={<span className="text-ink-2">RAM</span>}
            right={
              <span className={textSeverity[severity(frame.ram, THRESHOLDS.ram)]}>
                {formatPercent(frame.ram)}
              </span>
            }
          />
          <Graph label="RAM" values={history.ram} cols={graphCols} thresholds={THRESHOLDS.ram} />
          {(frame.rx > 0 || frame.tx > 0) && (
            <>
              <Divider />
              <Line
                left={<span className="text-ink-2">NET</span>}
                right={
                  <>
                    <span className="text-accent">↓</span>
                    {formatRate(frame.rx)} <span className="text-accent">↑</span>
                    {formatRate(frame.tx)}
                  </>
                }
              />
            </>
          )}
          {frame.disk !== null && (
            <>
              <Divider />
              <Line
                left={<span className="text-ink-2">DISK</span>}
                right={
                  <span className={textSeverity[severity(frame.disk, THRESHOLDS.disk)]}>
                    {formatPercent(frame.disk)}
                  </span>
                }
              />
            </>
          )}
        </>
      )}
    </article>
  );
}
