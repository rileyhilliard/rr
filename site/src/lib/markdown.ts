// Docs Markdown conventions, as Sätteri hast plugins (Astro's default processor)
// plus one Shiki transformer. astro.config.mjs registers them; markdown.test.ts
// renders through the same processor.
import type { SatteriProcessorOptions } from "@astrojs/markdown-satteri";
import type { ShikiConfig } from "astro";
import { rewriteDocsLink } from "./docs";

type HastPlugin = Extract<
  NonNullable<SatteriProcessorOptions["hastPlugins"]>[number],
  { name: string }
>;
type ShikiTransformer = NonNullable<ShikiConfig["transformers"]>[number];

const element = (tagName: string, properties: Record<string, unknown>, children: unknown[] = []) =>
  ({ type: "element", tagName, properties, children }) as never;
const text = (value: string) => ({ type: "text", value }) as never;

const CALLOUTS: Record<string, string> = {
  NOTE: "Note",
  TIP: "Tip",
  IMPORTANT: "Important",
  WARNING: "Warning",
  CAUTION: "Caution",
};

/** `> [!NOTE]` blockquotes (GitHub's alert syntax) become `<aside class="callout callout-note">`. */
export const callouts = {
  name: "callouts",
  element: {
    filter: ["blockquote"],
    visit(node) {
      const first = node.children.find((child) => child.type === "element");
      const lead =
        first?.type === "element" && first.tagName === "p" ? first.children[0] : undefined;
      const match = lead?.type === "text" ? /^\[!(\w+)\][ \t]*\n?/.exec(lead.value) : null;
      const label = match?.[1] ? CALLOUTS[match[1]] : undefined;
      if (first?.type !== "element" || lead?.type !== "text" || !match || !label) return;
      const rest = lead.value.slice(match[0].length);
      const leadChildren = [...(rest ? [text(rest)] : []), ...first.children.slice(1)];
      const children = node.children.map((child) =>
        child === first ? { ...first, children: leadChildren } : child,
      );
      return element("aside", { className: ["callout", `callout-${label.toLowerCase()}`] }, [
        element("p", { className: ["callout-title"] }, [text(label)]),
        ...children,
      ]);
    },
  },
} satisfies HastPlugin;

/** A link to each h2 and h3. It carries no text, so headings and the TOC keep their own. */
export const headingAnchors = {
  name: "heading-anchors",
  element: {
    filter: ["h2", "h3"],
    visit(node, ctx) {
      const id = node.properties.id;
      if (typeof id !== "string") return;
      ctx.appendChild(
        node,
        element("a", {
          className: ["heading-anchor"],
          href: `#${id}`,
          ariaLabel: "Link to this section",
        }),
      );
    },
  },
} satisfies HastPlugin;

/** Wide tables scroll inside a focusable region instead of widening the page. */
export const tableScroll = {
  name: "table-scroll",
  element: {
    filter: ["table"],
    visit(node, ctx) {
      ctx.wrapNode(
        node,
        element("div", {
          className: ["table-scroll"],
          tabIndex: 0,
          role: "region",
          ariaLabel: "Scrollable table",
        }),
      );
    },
  },
} satisfies HastPlugin;

/** Every code block sits in a figure (the copy button's anchor), captioned when it has a title. */
export const codeFrames = {
  name: "code-frames",
  element: {
    filter: ["pre"],
    visit(node) {
      // Set by codeTitle below; Shiki's hast keeps the attribute name as written.
      const title = node.properties["data-title"];
      const caption = typeof title === "string" ? [element("figcaption", {}, [text(title)])] : [];
      return element("figure", { className: ["code-frame"] }, [...caption, node]);
    },
  },
} satisfies HastPlugin;

/** Copies `title="..."` from a fence's info string onto the highlighted `<pre>` as data-title. */
export const codeTitle: ShikiTransformer = {
  name: "code-title",
  pre(node) {
    const raw = (this.options.meta as { __raw?: string } | undefined)?.__raw ?? "";
    const title = /title="([^"]+)"/.exec(raw)?.[1];
    if (title) node.properties["data-title"] = title;
  },
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
  callouts,
  headingAnchors,
  tableScroll,
  codeFrames,
];
