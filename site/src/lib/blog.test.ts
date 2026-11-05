import { describe, expect, it } from "vitest";
import { site } from "../config";
import { blogSchema } from "../content/blog-schema";
import { loadCommands } from "../data/commands";
import {
  breadcrumbs,
  collectionJsonLd,
  isDraft,
  llmsBlog,
  type Post,
  postHref,
  postImage,
  postJsonLd,
  publishedPosts,
  readingTime,
  recordedCommand,
  relatedDocs,
  relatedPosts,
} from "./blog";

const post = (id: string, published = "2026-11-01", feature = "parallel"): Post => ({
  id,
  data: blogSchema.parse({
    title: id,
    description: "x".repeat(120),
    published,
    feature,
    keyword: "remote tests",
  }),
});

describe("blog rules", () => {
  it("counts body words at 230 per minute, excluding metadata, imports and JSX tags", () => {
    expect(
      readingTime(
        `---\ntitle: ${"x ".repeat(500)}\n---\nimport Thing from "./thing";\n<Thing caption="${"x ".repeat(500)}" />\n${"word ".repeat(230)}`,
      ),
    ).toBe(1);
    expect(readingTime("word ".repeat(231))).toBe(2);
    expect(readingTime("")).toBe(1);
    expect(
      readingTime(`import Demo from "./demo"

<Demo caption="${"ignored ".repeat(500)} > end" />

${"word ".repeat(230)}`),
    ).toBe(1);
    expect(readingTime(`import {\n One,\n Two\n} from "./parts";\n${"word ".repeat(230)}`)).toBe(1);
  });
  it("reads draft flags only from the opening frontmatter block", () => {
    expect(isDraft("---\ntitle: Example\n---\n\n```yaml\ndraft: true\n```")).toBe(false);
    expect(isDraft("---\r\ndraft: true\r\n---\r\nBody")).toBe(true);
    expect(isDraft("draft: true")).toBe(false);
    expect(isDraft("---\ndraft: false\n---\nBody")).toBe(false);
  });

  it("filters drafts and sorts newest first without mutating the input", () => {
    const old = post("old");
    const fresh = post("fresh", "2026-11-02");
    const draft = post("draft");
    draft.data.draft = true;
    const input = [old, draft, fresh];
    expect(publishedPosts(input)).toEqual([fresh, old]);
    expect(input).toEqual([old, draft, fresh]);
    expect(publishedPosts([])).toEqual([]);
  });
  it("selects up to three other published posts, same feature before recency", () => {
    const current = post("current");
    const same = post("same");
    const other = post("other", "2026-11-03", "agents");
    const older = post("older", "2026-10-01");
    expect(
      relatedPosts(current, [other, current, same, older, post("last", "2026-09-01")]).map(
        (p) => p.id,
      ),
    ).toEqual(["same", "older", "last"]);
    expect(relatedPosts(current, [current, same, other]).map((p) => p.id)).toEqual([
      "same",
      "other",
    ]);
  });
  it("reports the post and bad docs field value", () => {
    const entry = post("broken");
    entry.data.relatedDocs = ["missing"];
    expect(() => relatedDocs(entry, [])).toThrow(
      "Post broken: relatedDocs 'missing' is not a docs page (have: )",
    );
  });
  it("builds absolute URLs, structured data without absent values, and final breadcrumbs without item", () => {
    const entry = post("example");
    expect(postHref(entry.id)).toBe("/blog/example/");
    expect(postImage(entry.id)).toBe("/og/blog/example.webp");
    const article = postJsonLd(entry, site.url, site.repo);
    expect(article).toMatchObject({
      "@id": `${site.url}/blog/example/#article`,
      headline: "example",
      dateModified: entry.data.published.toISOString(),
      author: { url: "https://github.com/rileyhilliard" },
      about: { "@id": `${site.url}/#software` },
    });
    entry.data.updated = new Date("2026-11-04");
    entry.data.headline = "Longer headline";
    expect(postJsonLd(entry, site.url, site.repo)).toMatchObject({
      headline: "Longer headline",
      dateModified: entry.data.updated.toISOString(),
    });
    const graph = [
      article,
      breadcrumbs(site.url, entry),
      collectionJsonLd([entry], site.url),
      breadcrumbs(site.url),
    ];
    const check = (value: unknown) => {
      expect(value).not.toBeNull();
      expect(value).not.toBeUndefined();
      if (typeof value === "object" && value) for (const item of Object.values(value)) check(item);
    };
    check(graph);
    expect(breadcrumbs(site.url, entry).itemListElement.at(-1)).not.toHaveProperty("item");
    expect(breadcrumbs(site.url).itemListElement.at(-1)).not.toHaveProperty("item");
    expect(collectionJsonLd([], site.url).mainEntity.itemListElement).toEqual([]);
    expect(llmsBlog([entry], site.url)).toContain(
      `- [example](${site.url}/blog/example/): ${entry.data.description}`,
    );
  });
  it("rejects an unknown command using the real recording manifest", () => {
    const commands = loadCommands();
    expect(() => recordedCommand(commands, "typo")).toThrow(
      "CommandDemo: unknown command 'typo' (allowed: init | doctor | status | run | test | logs)",
    );
    expect(recordedCommand(commands, "doctor").command).toBe("rr doctor");
  });
});
