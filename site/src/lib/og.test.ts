// @vitest-environment node
import satori from "satori";
import { describe, expect, it } from "vitest";
import { clip, ogCard, ogFonts, ogLogo } from "./og";

describe("social cards", () => {
  it("clips long text on a word boundary", () => {
    expect(clip("short", 10)).toBe("short");
    expect(clip("one two three four", 12)).toBe("one two…");
  });

  it("renders the title with the bundled fonts and the brand lockup", async () => {
    const card = ogCard({
      title: "Installation",
      description: "Install it.",
      siteName: "Acme",
      logo: await ogLogo(),
    });
    const svg = await satori(card as never, { width: 1200, height: 630, fonts: await ogFonts() });
    expect(svg).toMatch(/^<svg[^>]+width="1200"[^>]+height="630"/);
    expect(svg).toContain("<path");
    expect(svg).toContain("data:image/svg+xml;base64,");
  });
});
