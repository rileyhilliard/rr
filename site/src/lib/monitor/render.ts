// A port of the rr monitor's text rendering (internal/monitor/graphs.go, styles.go,
// view.go, types.go), so the landing page draws the same characters the TUI does.
// fixtures/monitor-golden.json holds the Go output for fixed inputs; render.test.ts
// checks this port against it, and internal/monitor/site_golden_test.go checks Go.

export type Severity = "healthy" | "warning" | "critical";
export type Thresholds = { warning: number; critical: number };

/** The monitor's default thresholds (config defaults, and disk's fixed pair). */
export const THRESHOLDS = {
  cpu: { warning: 70, critical: 90 },
  ram: { warning: 70, critical: 90 },
  gpu: { warning: 70, critical: 90 },
  disk: { warning: 80, critical: 95 },
} satisfies Record<string, Thresholds>;

/** MetricColorWithThresholds: cyan, purple, pink in the TUI. */
export function severity(percent: number, t: Thresholds): Severity {
  if (percent >= t.critical) return "critical";
  if (percent >= t.warning) return "warning";
  return "healthy";
}

export type LatencyBand = "fast" | "normal" | "slow" | "degraded";

/** LatencyColor and LatencyQualityText share these bands. */
export function latencyBand(ms: number): LatencyBand {
  if (ms >= 500) return "degraded";
  if (ms >= 200) return "slow";
  if (ms >= 50) return "normal";
  return "fast";
}

const BRAILLE_BASE = 0x2800;
// [row][col] → bit offset, rows top to bottom.
const BRAILLE_DOTS = [
  [0, 3],
  [1, 4],
  [2, 5],
  [6, 7],
];

/** ResampleDataWithMode(ResampleMax): bucket max when shrinking, linear when growing. */
export function resample(data: readonly number[], target: number): number[] {
  if (data.length === 0 || target <= 0) return [];
  if (data.length === target) return [...data];
  if (data.length === 1) return Array(target).fill(data[0]);
  const result: number[] = [];
  if (data.length > target) {
    const bucket = data.length / target;
    for (let i = 0; i < target; i++) {
      let start = Math.trunc(i * bucket);
      const end = Math.min(Math.trunc((i + 1) * bucket), data.length);
      if (start >= end) start = end - 1;
      if (start < 0) start = 0;
      let max = data[start] ?? 0;
      for (let j = start + 1; j < end; j++) max = Math.max(max, data[j] ?? 0);
      result.push(max);
    }
    return result;
  }
  const scale = (data.length - 1) / (target - 1);
  for (let i = 0; i < target; i++) {
    const pos = i * scale;
    const idx = Math.trunc(pos);
    const frac = pos - idx;
    result.push(
      idx >= data.length - 1
        ? (data[data.length - 1] ?? 0)
        : (data[idx] ?? 0) * (1 - frac) + (data[idx + 1] ?? 0) * frac,
    );
  }
  return result;
}

/** SmoothWithMovingAverage, used on latency before plotting. */
export function smooth(data: readonly number[], window: number): number[] {
  if (data.length === 0 || window <= 1) return [...data];
  const half = Math.trunc(window / 2);
  return data.map((_, i) => {
    const start = Math.max(0, i - half);
    const end = Math.min(data.length, i + half + 1);
    let sum = 0;
    for (let j = start; j < end; j++) sum += data[j] ?? 0;
    return sum / (end - start);
  });
}

export type Sparkline = {
  /** One string per row, top to bottom, each `width` braille characters. */
  rows: string[];
  /** The max value plotted in each character column, for coloring. */
  columnMax: number[];
};

/**
 * RenderBrailleSparklineWithOptions without the styling: two points per character,
 * four dot levels per row, right-aligned when there's less data than width.
 */
export function sparkline(
  data: readonly number[],
  width: number,
  height: number,
  forceZeroMin = false,
): Sparkline {
  if (data.length === 0 || width <= 0 || height <= 0) return { rows: [], columnMax: [] };
  let min = Math.min(...data);
  let max = Math.max(...data);
  const isPercentage = max <= 100 && min >= 0;
  if (isPercentage) {
    min = 0;
    max = 100;
  }
  if (forceZeroMin && !isPercentage) min = 0;
  const totalDots = height * 4;
  const targetPoints = width * 2;
  const points = data.length > targetPoints ? resample(data, targetPoints) : data;
  const grid = Array.from({ length: height }, () => Array<number>(width).fill(BRAILLE_BASE));
  const columnMax = Array<number>(width).fill(0);
  const offset = Math.max(0, targetPoints - points.length);

  points.forEach((value, i) => {
    const normalized = max > min ? (value - min) / (max - min) : 0.5;
    const dotHeight = Math.min(Math.max(Math.trunc(normalized * totalDots), 0), totalDots);
    const col = Math.trunc((i + offset) / 2);
    if (col >= width) return;
    if (value > (columnMax[col] ?? 0)) columnMax[col] = value;
    const subCol = (i + offset) % 2;
    for (let dot = 0; dot < dotHeight; dot++) {
      const row = height - 1 - Math.trunc(dot / 4);
      if (row < 0) continue;
      const bit = BRAILLE_DOTS[3 - (dot % 4)]?.[subCol] ?? 0;
      const line = grid[row];
      if (line) line[col] = (line[col] ?? BRAILLE_BASE) | (1 << bit);
    }
  });

  return { rows: grid.map((row) => String.fromCodePoint(...row)), columnMax };
}

/** RenderGradientBarWithColorFunc's characters: ▰ filled, ▱ empty. */
export function gradientBar(width: number, percent: number): string {
  const w = Math.max(1, width);
  const p = Math.min(Math.max(percent, 0), 100);
  const filled = Math.min(Math.trunc((p / 100) * w), w);
  return "▰".repeat(filled) + "▱".repeat(w - filled);
}

/** The per-position percentage RenderGradientBar colors each filled cell by. */
export const barCellPercent = (index: number, width: number) => ((index + 1) / width) * 100;

/** RenderCoreHeatStrip's characters: one ▰ per core, capped at maxWidth. */
export function heatStrip(perCore: readonly number[], maxWidth: number): string {
  if (perCore.length === 0 || maxWidth <= 0) return "";
  return "▰".repeat(Math.min(perCore.length, maxWidth));
}

/** Go's %.Nf: like toFixed, except an exact tie rounds to even (7.25 → "7.2"). */
function goFixed(value: number, digits: number): string {
  const scale = 10 ** digits;
  const scaled = value * scale;
  const floor = Math.floor(scaled);
  if (scaled - floor === 0.5 && floor % 2 === 0) return (floor / scale).toFixed(digits);
  return value.toFixed(digits);
}

/** FormatRate. */
export function formatRate(bytesPerSecond: number): string {
  if (bytesPerSecond < 1024) return `${goFixed(bytesPerSecond, 0)} B/s`;
  if (bytesPerSecond < 1024 * 1024) return `${goFixed(bytesPerSecond / 1024, 1)} KB/s`;
  if (bytesPerSecond < 1024 ** 3) return `${goFixed(bytesPerSecond / 1024 ** 2, 1)} MB/s`;
  return `${goFixed(bytesPerSecond / 1024 ** 3, 1)} GB/s`;
}

/** formatBytes. */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  let div = 1024;
  let exp = 0;
  for (let n = Math.trunc(bytes / 1024); n >= 1024; n = Math.trunc(n / 1024)) {
    div *= 1024;
    exp++;
  }
  return `${goFixed(bytes / div, 1)} ${["KB", "MB", "GB", "TB", "PB"][exp]}`;
}

/** HostLockInfo.FormatDuration: "0s", "41s", "2m", "2m30s". */
export function formatLockDuration(ms: number): string {
  if (ms < 1000) return "0s";
  const minutes = Math.trunc(ms / 60_000);
  const seconds = Math.trunc(ms / 1000) % 60;
  if (minutes > 0) return seconds > 0 ? `${minutes}m${seconds}s` : `${minutes}m`;
  return `${seconds}s`;
}

/** The card's "%5.1f%%" percentage. */
export const formatPercent = (percent: number) => `${goFixed(percent, 1).padStart(5)}%`;

/** truncateWithEllipsis. */
export function truncate(value: string, max: number): string {
  if (max <= 3) return value;
  return value.length > max ? `${value.slice(0, max - 3)}...` : value;
}

/** RunningSpinnerFrames and the amber SpinnerColorFrames they cycle through. */
export const RUNNING_FRAMES = ["⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"];
export const SPINNER_COLORS = [
  "#FFAA00",
  "#FF8800",
  "#FFCC00",
  "#FFAA00",
  "#FF9900",
  "#FFBB00",
  "#FFAA00",
  "#FF7700",
];

export const GLYPHS = { idle: "◉", unreachable: "◌", connecting: "◐" } as const;

/**
 * Alert hysteresis (alerts.go, with monitor.alerts.enabled): a metric starts firing
 * when it reaches critical, keeps firing between warning and critical, and re-arms
 * below warning. A firing host's card shows the critical border.
 */
export function alertFiring(series: readonly number[], t: Thresholds): boolean[] {
  let firing = false;
  return series.map((value) => {
    if (value < t.warning) firing = false;
    else if (value >= t.critical) firing = true;
    return firing;
  });
}
