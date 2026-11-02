// @vitest-environment node
import { createSatteriMarkdownProcessor, satteriHeadingIdsPlugin } from "@astrojs/markdown-satteri";
import { describe, expect, it } from "vitest";
import { codeTitle, docsHastPlugins } from "./markdown";

async function render(markdown: string) {
  const processor = await createSatteriMarkdownProcessor({
    hastPlugins: [satteriHeadingIdsPlugin(), ...docsHastPlugins],
    syntaxHighlight: "shiki",
    shikiConfig: {
      themes: { light: "github-light", dark: "github-dark-default" },
      transformers: [codeTitle],
    },
  });
  return processor.render(markdown);
}

describe("docs Markdown", () => {
  it("turns GitHub alerts into labelled callouts and leaves plain quotes alone", async () => {
    const { code } = await render("> [!WARNING]\n> Back up first.\n\n> Just a quote.\n");
    expect(code).toContain('<aside class="callout callout-warning">');
    expect(code).toContain('<p class="callout-title">Warning</p>');
    expect(code).toContain("<p>Back up first.</p>");
    expect(code).not.toContain("[!WARNING]");
    expect(code).toContain("<blockquote>");
  });

  it("links headings without changing their text", async () => {
    const { code, metadata } = await render("## Install it\n");
    expect(code).toContain('<a class="heading-anchor" href="#install-it"');
    expect(metadata.headings).toEqual([{ depth: 2, slug: "install-it", text: "Install it" }]);
  });

  it("frames code blocks and captions titled ones", async () => {
    const { code } = await render('```sh title="Terminal"\nbun add acme\n```\n\n```js\nx\n```\n');
    expect(code.match(/<figure class="code-frame">/g)).toHaveLength(2);
    expect(code).toContain("<figcaption>Terminal</figcaption>");
  });

  it("wraps tables in a focusable scroll region", async () => {
    const { code } = await render("| a | b |\n| - | - |\n| 1 | 2 |\n");
    expect(code).toMatch(/<div class="table-scroll" tabindex="0" role="region"[^>]*><table>/);
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
