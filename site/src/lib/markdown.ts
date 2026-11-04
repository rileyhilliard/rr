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

/** Code blocks' copy buttons say "Copy", as rr's always have; site.ts's enhanceCopyButtons wires them. */
const COPY = "Copy";

/**
 * Shiki copies `title="..."` from a fence's info string onto the `<pre>` (codeMeta), and
 * codeBlocks turns it into the frame's caption.
 */
export const shikiTransformers = [codeMeta()];

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
  headingAnchors({ label: () => "Link to this section" }),
  tables(),
  codeBlocks({ copyLabel: COPY, copyText: COPY }),
];
