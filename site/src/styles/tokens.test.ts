// @vitest-environment node
// WCAG contrast for every token pair the site draws text or controls with. The
// palette is rr's TUI palette, and neon on near-black is easy to get wrong.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("./global.css", import.meta.url), "utf8");
const token = (name: string) => {
  const value = new RegExp(`--color-${name}:\\s*(#[0-9a-f]{6})`, "i").exec(css)?.[1];
  if (!value) throw new Error(`--color-${name} missing from global.css`);
  return value;
};

function luminance(hex: string) {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = Number.parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * (r ?? 0) + 0.7152 * (g ?? 0) + 0.0722 * (b ?? 0);
}

const contrast = (a: string, b: string) => {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return ((hi ?? 0) + 0.05) / ((lo ?? 0) + 0.05);
};

const surfaces = ["canvas", "panel"];
const text = [
  "ink",
  "ink-2",
  "muted",
  "accent",
  "info",
  "success",
  "waiting",
  "error",
  "sync",
  "lat-normal",
  "lat-slow",
];

describe("token contrast", () => {
  it.each(surfaces.flatMap((surface) => text.map((fg) => [fg, surface])))(
    "%s text on %s is at least 4.5:1",
    (fg, surface) => {
      expect(contrast(token(fg), token(surface))).toBeGreaterThanOrEqual(4.5);
    },
  );

  it.each(surfaces)("control borders (line-strong) on %s are at least 3:1", (surface) => {
    expect(contrast(token("line-strong"), token(surface))).toBeGreaterThanOrEqual(3);
  });

  it.each(["accent", "info", "success", "waiting", "error", "ink"])(
    "canvas text on a %s fill is at least 4.5:1",
    (fill) => {
      expect(contrast(token("canvas"), token(fill))).toBeGreaterThanOrEqual(4.5);
    },
  );

  it("keeps faint below AA, so it stays decorative", () => {
    expect(contrast(token("faint"), token("canvas"))).toBeLessThan(4.5);
  });
});
