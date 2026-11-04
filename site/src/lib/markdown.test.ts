// @vitest-environment node
import { Window } from "happy-dom";
import { describe, expect, it } from "vitest";
import { renderDocsMarkdown as render } from "../../tests/helpers/markdown";

const parser = new new Window().DOMParser();
async function dom(markdown: string) {
  const { code } = await render(markdown);
  return parser.parseFromString(`<body>${code}</body>`, "text/html").body;
}

// Markup-agnostic: what a reader and assistive tech get, whatever classes or data attributes
// carry the styling.
describe("rendered docs Markdown", () => {
  it("gives every GitHub alert kind a visible title ahead of its body, and leaves plain quotes alone", async () => {
    const kinds = {
      NOTE: "Note",
      TIP: "Tip",
      IMPORTANT: "Important",
      WARNING: "Warning",
      CAUTION: "Caution",
    };
    for (const [marker, title] of Object.entries(kinds)) {
      const body = await dom(`> [!${marker}]\n> Back up first.\n\n> Just a quote.\n`);
      const [callout, quote] = [...body.children];
      const paragraphs = [...(callout?.querySelectorAll("p") ?? [])].map((p) => p.textContent);
      expect(paragraphs).toEqual([title, "Back up first."]);
      expect(body.textContent).not.toContain(`[!${marker}]`);
      expect(quote?.tagName).toBe("BLOCKQUOTE");
      expect(quote?.textContent?.trim()).toBe("Just a quote.");
    }
  });

  it("frames every code block, captions a titled one, and keeps both Shiki themes", async () => {
    const body = await dom('```sh title="Terminal"\nbun add acme\n```\n\n```js\nx\n```\n');
    const figures = [...body.querySelectorAll("figure")];
    expect(figures).toHaveLength(2);
    expect(figures[0]?.querySelector("figcaption")?.textContent).toBe("Terminal");
    expect(figures[0]?.querySelector("pre code")?.textContent).toBe("bun add acme");
    expect(figures[1]?.querySelector("figcaption")).toBeNull();
    expect(figures[0]?.querySelector("pre")?.getAttribute("style")).toMatch(/--shiki-dark/);
  });

  it("puts each table in a labelled, focusable scroll region", async () => {
    const body = await dom("## Hosts\n\n| a | b |\n| - | - |\n| 1 | 2 |\n");
    const region = body.querySelector("table")?.parentElement;
    expect(region?.getAttribute("tabindex")).toBe("0");
    expect(region?.getAttribute("role")).toBe("region");
    // The library's default: named after the section, so two tables never share a name.
    expect(region?.getAttribute("aria-label")).toBe("Hosts table");
  });

  it("links each h2 and h3 to itself with a named, textless anchor", async () => {
    const body = await dom("## Install it\n\n### Then this\n");
    for (const [heading, id] of [
      ["h2", "install-it"],
      ["h3", "then-this"],
    ] as const) {
      const el = body.querySelector(heading);
      const link = el?.querySelector("a");
      expect(el?.id).toBe(id);
      expect(link?.getAttribute("href")).toBe(`#${id}`);
      expect(link?.getAttribute("aria-label")).toBe(`Link to section: ${el?.textContent}`);
      expect(link?.textContent).toBe("");
    }
  });
});

// The data-* hooks global.css styles and site.ts's copy enhancer reads.
describe("docs Markdown", () => {
  it("turns GitHub alerts into labelled callouts and leaves plain quotes alone", async () => {
    const body = await dom("> [!WARNING]\n> Back up first.\n\n> Just a quote.\n");
    const callout = body.querySelector('blockquote[role="note"][data-callout="warning"]');
    expect(callout?.querySelector("[data-callout-title]")?.textContent).toBe("Warning");
    expect(body.querySelectorAll("[data-callout]")).toHaveLength(1);
    expect(body.querySelector("blockquote:not([data-callout])")?.textContent?.trim()).toBe(
      "Just a quote.",
    );
  });

  it("links headings without changing their text", async () => {
    const { metadata } = await render("## Install it\n");
    const body = await dom("## Install it\n");
    expect(body.querySelector('h2 > a[data-heading-anchor][href="#install-it"]')).not.toBeNull();
    expect(metadata.headings).toEqual([{ depth: 2, slug: "install-it", text: "Install it" }]);
  });

  it("frames code blocks, captions titled ones, and gives each a copy control for its <pre>", async () => {
    const body = await dom('```sh title="Terminal"\nbun add acme\n```\n\n```js\nx\n```\n');
    const figures = [...body.querySelectorAll("figure[data-code-block]")];
    expect(figures).toHaveLength(2);
    expect(figures[0]?.querySelector("figcaption[data-code-title]")?.textContent).toBe("Terminal");
    expect(figures[1]?.querySelector("[data-code-title]")).toBeNull();
    // The caption leads the frame in the markup, so no CSS reorders it.
    expect(figures[0]?.firstElementChild?.tagName).toBe("FIGCAPTION");
    for (const [i, figure] of figures.entries()) {
      const pre = figure.querySelector("pre");
      const button = figure.querySelector("[data-code-copy] button[data-copy-command]");
      expect(pre?.id).toBe(`code-block-${i + 1}`);
      expect(button?.hasAttribute("hidden")).toBe(true);
      expect(button?.getAttribute("aria-controls")).toBe(pre?.id);
      expect(button?.getAttribute("aria-label")).toBe("Copy code");
      expect(button?.querySelector("[data-copy-label]")?.textContent).toBe("Copy");
      expect(
        button?.parentElement?.querySelector(':scope > [data-copy-result][role="status"]'),
      ).not.toBeNull();
    }
  });

  it("wraps tables in a focusable scroll region", async () => {
    const body = await dom("| a | b |\n| - | - |\n| 1 | 2 |\n");
    expect(
      body.querySelector('div[data-table-scroll][tabindex="0"][role="region"] > table'),
    ).not.toBeNull();
  });

  it("drops the file's own h1, which the docs layout renders from frontmatter", async () => {
    const { code } = await render("# SSH setup\n\nIntro.\n\n## Keys\n");
    expect(code).not.toContain("<h1");
    expect(code).toContain("<p>Intro.</p>");
  });

  it("points GitHub-style links at site routes or GitHub", async () => {
    const { code } = await render(
      "[a](ssh-setup.md) [b](configuration.md#local-host) [c](ARCHITECTURE.md) [d](../README.md#install) [e](#here) [f](https://x.dev)\n",
    );
    expect(code).toContain('href="/docs/ssh-setup/"');
    expect(code).toContain('href="/docs/configuration/#local-host"');
    expect(code).toContain(
      'href="https://github.com/rileyhilliard/rr/blob/main/docs/ARCHITECTURE.md"',
    );
    expect(code).toContain(
      'href="https://github.com/rileyhilliard/rr/blob/main/README.md#install"',
    );
    expect(code).toContain('href="#here"');
    expect(code).toContain('href="https://x.dev"');
  });
});
