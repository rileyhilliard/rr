// The social card satori renders for each page. satori lays out flexbox only
// (no grid) and reads TTF, OTF or WOFF fonts, never WOFF2.
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { join } from "node:path";

export type Card = { title: string; description: string; siteName: string };
/** A card plus the brand lockup as a data URI (ogLogo). */
export type CardWithLogo = Card & { logo: string };
type Node = { type: string; props: { style?: Record<string, unknown>; children?: unknown } };

const div = (style: Record<string, unknown>, children: unknown): Node => ({
  type: "div",
  props: { style, children },
});

/** Clips text to `max` characters on a word boundary so long titles never overflow the card. */
export function clip(value: string, max: number): string {
  if (value.length <= max) return value;
  const cut = value.slice(0, max - 1);
  return `${cut.slice(0, cut.lastIndexOf(" ") > 0 ? cut.lastIndexOf(" ") : cut.length)}…`;
}

export function ogCard({ title, description, siteName, logo }: CardWithLogo): Node {
  return div(
    {
      width: "100%",
      height: "100%",
      // Without it the padding is added to the canvas size and the text runs off the card.
      boxSizing: "border-box",
      display: "flex",
      flexDirection: "column",
      justifyContent: "space-between",
      padding: "72px",
      background: "#0a0a0f",
      color: "#ffffff",
      fontFamily: "Instrument Sans",
    },
    [
      // The lockup is 41.41x26 in its viewBox; satori needs both dimensions.
      { type: "img", props: { src: logo, alt: siteName, width: 115, height: 72 } },
      div({ display: "flex", flexDirection: "column", gap: "24px" }, [
        div({ fontSize: 68, fontWeight: 700, lineHeight: 1.1 }, clip(title, 80)),
        div({ fontSize: 30, color: "#b4b4d0", lineHeight: 1.4 }, clip(description, 160)),
      ]),
    ],
  );
}

/**
 * The dark horizontal lockup from public/brand/, as a data URI satori can draw. Read
 * from the project root: the build bundles this module, so import.meta.url moves.
 */
export async function ogLogo(): Promise<string> {
  const svg = await readFile(join(process.cwd(), "public/brand/rr-lockup-dark.svg"));
  return `data:image/svg+xml;base64,${svg.toString("base64")}`;
}

/** Instrument Sans from the installed @fontsource package, read at build time; nothing is fetched. */
export async function ogFonts() {
  const require = createRequire(import.meta.url);
  const load = (weight: 400 | 700) =>
    readFile(
      require.resolve(
        `@fontsource/instrument-sans/files/instrument-sans-latin-${weight}-normal.woff`,
      ),
    );
  const [regular, bold] = await Promise.all([load(400), load(700)]);
  return [
    { name: "Instrument Sans", data: regular, weight: 400 as const, style: "normal" as const },
    { name: "Instrument Sans", data: bold, weight: 700 as const, style: "normal" as const },
  ];
}
