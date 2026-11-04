// Docs Markdown conventions, as Sätteri hast plugins (Astro's default processor)
// plus one Shiki transformer. Callouts, heading anchors, table scroll regions and code
// frames come from @steamfield/ui/markdown; the GitHub-link rewrite and the h1 drop are
// rr's. astro.config.mjs registers them; markdown.test.ts renders through the same processor.
import type { SatteriProcessorOptions } from "@astrojs/markdown-satteri";
import { callouts, codeBlocks, codeMeta, headingAnchors, tables } from "@steamfield/ui/markdown";
import { rewriteDocsLink } from "./docs";

type HastPlugin = Extract<
  NonNullable<SatteriProcessorOptions["hastPlugins"]>[number],
  { name: string }
>;

/** rr's code colors: both GitHub themes as CSS variables; global.css picks the dark one. */
export const shikiThemes = { light: "github-light", dark: "github-dark-default" } as const;

/**
 * Shiki settings for the docs and the landing page's code frames. codeMeta copies
 * `title="..."` from a fence's info string onto the `<pre>`, and codeBlocks turns it into
 * the frame's caption. Not the library's createShikiConfig, which uses one css-variables theme.
 */
export const shikiConfig = {
  themes: shikiThemes,
  defaultColor: false as const,
  transformers: [codeMeta()],
};

export const docsLinks = {
  name: "docs-links",
  element: {
    filter: ["a"],
    visit(node, ctx) {
      const href = node.properties.href;
      if (typeof href !== "string") return;
      const next = rewriteDocsLink(href);
      if (next !== href) ctx.setProperty(node, "href", next);
    },
  },
} satisfies HastPlugin;

/** Each docs file opens with its own `# Title` for GitHub; the docs layout renders the h1. */
export const dropTitle = {
  name: "drop-title",
  element: {
    filter: ["h1"],
    visit(node, ctx) {
      ctx.removeNode(node);
    },
  },
} satisfies HastPlugin;

export const docsHastPlugins = [
  dropTitle,
  docsLinks,
  callouts(),
  headingAnchors(),
  tables(),
  // "code-block-N" keeps the <pre> ids clear of heading slugs such as #code-1.
  codeBlocks({ copyText: "Copy", idPrefix: "code-block" }),
];
