import { describe, expect, it } from "vitest";
import {
  type DocEntry,
  docsNav,
  llmsFull,
  llmsIndex,
  pageLinks,
  rewriteDocsLink,
  siteMarkdown,
  toMarkdownExport,
} from "./docs";

const groups = [
  { id: "getting-started", label: "Getting started" },
  { id: "concepts", label: "Concepts" },
  { id: "reference", label: "Reference" },
];
const doc = (id: string, data: Partial<DocEntry["data"]>): DocEntry => ({
  id,
  data: { title: id, description: `About ${id}`, group: "getting-started", order: 100, ...data },
  body: `Body of ${id}.`,
});
const entries = [
  doc("reference/config", { title: "Configuration", group: "reference" }),
  doc("getting-started/later", { title: "Later", order: 2 }),
  doc("getting-started/install", { title: "Installation", order: 1, sidebarLabel: "Install" }),
];
const site = { name: "Acme", tagline: "Notes search.", url: "https://example.com" };

describe("docsNav", () => {
  it("orders groups by config and pages by order, dropping empty groups", () => {
    const nav = docsNav(entries, groups);
    expect(nav.map((group) => group.id)).toEqual(["getting-started", "reference"]);
    expect(nav[0]?.items.map((item) => item.label)).toEqual(["Install", "Later"]);
  });

  it("breaks order ties by title", () => {
    const tied = [doc("b", { title: "Beta" }), doc("a", { title: "Alpha" })];
    expect(docsNav(tied, groups)[0]?.items.map((item) => item.id)).toEqual(["a", "b"]);
  });
});

describe("pageLinks", () => {
  const nav = docsNav(entries, groups);

  it("links across group boundaries", () => {
    const links = pageLinks(nav, "getting-started/later");
    expect(links.prev?.id).toBe("getting-started/install");
    expect(links.next?.id).toBe("reference/config");
  });

  it("has no previous page at the start and none for an unknown id", () => {
    expect(pageLinks(nav, "getting-started/install").prev).toBeUndefined();
    expect(pageLinks(nav, "missing")).toEqual({ prev: undefined, next: undefined });
  });
});

describe("agent-readable exports", () => {
  const nav = docsNav(entries, groups);

  it("exports a page as Markdown with its title and description", () => {
    expect(toMarkdownExport(entries[2] as DocEntry)).toBe(
      "# Installation\n\n> About getting-started/install\n\nBody of getting-started/install.\n",
    );
  });

  it("lists every page in llms.txt by group with absolute Markdown URLs", () => {
    const text = llmsIndex(site, nav);
    expect(text).toContain("# Acme\n\n> Notes search.");
    expect(text).toContain(
      "## Reference\n\n- [Configuration](https://example.com/docs/reference/config.md): About reference/config",
    );
  });

  it("concatenates every page in sidebar order in llms-full.txt", () => {
    const text = llmsFull(site, nav, entries);
    expect(text.indexOf("# Installation")).toBeLessThan(text.indexOf("# Later"));
    expect(text.indexOf("# Later")).toBeLessThan(text.indexOf("# Configuration"));
  });
});

describe("docs from the repository's docs/ folder", () => {
  it("serves the body without its own title and with site links", () => {
    expect(
      siteMarkdown(
        "# SSH setup\n\nSee [config](configuration.md#lock) and [arch](ARCHITECTURE.md).\n",
      ),
    ).toBe(
      "See [config](/docs/configuration/#lock) and [arch](https://github.com/rileyhilliard/rr/blob/main/docs/ARCHITECTURE.md).\n",
    );
  });

  it("maps MIGRATION.md to its lowercased route and leaves absolute links alone", () => {
    expect(rewriteDocsLink("MIGRATION.md")).toBe("/docs/migration/");
    expect(rewriteDocsLink("examples/go-project")).toBe(
      "https://github.com/rileyhilliard/rr/blob/main/docs/examples/go-project",
    );
    expect(rewriteDocsLink("mailto:a@b.c")).toBe("mailto:a@b.c");
    expect(rewriteDocsLink("/docs/x/")).toBe("/docs/x/");
  });
});
