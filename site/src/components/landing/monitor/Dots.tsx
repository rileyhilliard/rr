// Draws braille rows from render.ts as SVG dots. The site's fonts have no braille
// or ▰▱ glyphs, and a fallback font would knock the columns out of line, so the
// dots the TUI lights are drawn directly. A sparkline fills each braille
// sub-column from the bottom up, so every sub-column is one dashed vertical line
// (the dash pattern makes the dots), grouped into one path per color.
import type { Severity } from "../../../lib/monitor/render";

// Bit offsets per [row][col] inside one braille cell (graphs.go brailleDots).
const CELL_BITS = [
  [0, 3],
  [1, 4],
  [2, 5],
  [6, 7],
];

export const severityClass: Record<Severity, string> = {
  healthy: "stroke-info",
  warning: "stroke-sync",
  critical: "stroke-accent",
};

/** Lit dots per sub-column (two per braille character), counted across all rows. */
export function subColumnHeights(rows: string[]): number[] {
  const width = [...(rows[0] ?? "")].length;
  const heights = Array<number>(width * 2).fill(0);
  for (const row of rows) {
    [...row].forEach((char, col) => {
      const bits = (char.codePointAt(0) ?? 0x2800) - 0x2800;
      for (const pair of CELL_BITS) {
        pair.forEach((bit, sub) => {
          if (bits & (1 << bit)) heights[col * 2 + sub] = (heights[col * 2 + sub] ?? 0) + 1;
        });
      }
    });
  }
  return heights;
}

type Props = {
  rows: string[];
  /** A class per character column, from its plotted max. */
  columnClass: (col: number) => string;
  label: string;
};

export function BrailleDots({ rows, columnClass, label }: Props) {
  const heights = subColumnHeights(rows);
  if (heights.length === 0) return null;
  const total = rows.length * 4;
  const paths = new Map<string, string>();
  heights.forEach((height, sub) => {
    if (height === 0) return;
    const className = columnClass(Math.floor(sub / 2));
    // From the bottom dot's lower edge up to the top dot's upper edge.
    const segment = `M${sub + 0.5} ${total - 0.18}V${total - height + 0.18}`;
    paths.set(className, (paths.get(className) ?? "") + segment);
  });
  return (
    <svg
      role="img"
      aria-label={label}
      viewBox={`0 0 ${heights.length} ${total}`}
      preserveAspectRatio="none"
      className="block h-auto w-full"
      style={{ aspectRatio: `${heights.length} / ${total * 1.15}` }}
    >
      {[...paths].map(([className, d]) => (
        <path
          key={className}
          d={d}
          className={className}
          fill="none"
          strokeWidth={0.64}
          strokeDasharray="0.64 0.36"
          strokeLinecap="butt"
        />
      ))}
    </svg>
  );
}

const fillClass: Record<string, string> = {
  "stroke-info": "fill-info",
  "stroke-sync": "fill-sync",
  "stroke-accent": "fill-accent",
};

/** RenderGradientBar: filled cells colored by their position's severity, the rest muted. */
export function UsageBar({
  cells,
  filled,
  cellClass,
}: {
  cells: number;
  filled: number;
  cellClass: (index: number) => string;
}) {
  return (
    <svg
      aria-hidden="true"
      viewBox={`0 0 ${cells * 2} 2`}
      preserveAspectRatio="none"
      className="block h-2 w-full"
    >
      {Array.from({ length: cells }, (_, i) => (
        <rect
          // biome-ignore lint/suspicious/noArrayIndexKey: cells are positional and never reorder
          key={i}
          x={i * 2 + 0.2}
          y={0}
          width={1.6}
          height={2}
          rx={0.3}
          className={i < filled ? (fillClass[cellClass(i)] ?? cellClass(i)) : "fill-faint"}
        />
      ))}
    </svg>
  );
}
