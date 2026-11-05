import { type MdastNode, mdxToMdast } from "satteri";
import type { BlogData } from "../content/blog-schema";
import type { DocEntry } from "./docs";

export type Post = { id: string; data: BlogData; body?: string };
export const blogTitle = "Remote test runs and coding agents";
export const blogDescription =
  "Practical guides to running commands over SSH, sharing remote hosts, and keeping test runs moving while several coding agents work at once.";
export const postHref = (id: string) => `/blog/${id}/`;
export const postImage = (id: string) => `/og/blog/${id}.webp`;
const absolute = (path: string, url: string) => new URL(path, url).href;

/** Whether the post declares itself a draft. */
export function isDraft(source: string): boolean {
  const frontmatter = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/.exec(source)?.[1] ?? "";
  return /^draft:\s*true\s*$/m.test(frontmatter);
}

/** Count the MDX body, excluding frontmatter, imports and JSX markup. */
export function readingTime(source: string): number {
  let words = 0;
  const count = (node: MdastNode) => {
    if (node.type === "text" || node.type === "code" || node.type === "inlineCode")
      words += node.value.match(/\S+/g)?.length ?? 0;
    if ("children" in node) for (const child of node.children) count(child);
  };
  count(mdxToMdast(source));
  return Math.max(1, Math.ceil(words / 230));
}

export function publishedPosts<T extends Post>(posts: readonly T[]): T[] {
  return posts
    .filter((post) => !post.data.draft)
    .sort(
      (a, b) => b.data.published.getTime() - a.data.published.getTime() || a.id.localeCompare(b.id),
    );
}

export function relatedPosts(post: Post, posts: readonly Post[]): Post[] {
  return publishedPosts(posts)
    .filter((other) => other.id !== post.id)
    .sort(
      (a, b) =>
        Number(b.data.feature === post.data.feature) - Number(a.data.feature === post.data.feature),
    )
    .slice(0, 3);
}

export function relatedDocs(post: Post, docs: readonly DocEntry[]): DocEntry[] {
  return post.data.relatedDocs.map((id) => {
    const doc = docs.find((entry) => entry.id === id);
    if (!doc)
      throw new Error(
        `Post ${post.id}: relatedDocs '${id}' is not a docs page (have: ${docs
          .map((entry) => entry.id)
          .sort()
          .join(", ")})`,
      );
    return doc;
  });
}

export function breadcrumbs(url: string, post?: Post) {
  const items = [
    { name: "Home", item: absolute("/", url) },
    { name: "Blog", ...(post ? { item: absolute("/blog/", url) } : {}) },
    ...(post ? [{ name: post.data.title }] : []),
  ];
  return {
    "@context": "https://schema.org",
    "@type": "BreadcrumbList",
    itemListElement: items.map((item, index) => ({
      "@type": "ListItem",
      position: index + 1,
      ...item,
    })),
  };
}

export function postJsonLd(post: Post, url: string, repo: string) {
  const canonical = absolute(postHref(post.id), url);
  const person = {
    "@type": "Person",
    name: "Riley Hilliard",
    url: new URL(".", repo).href.replace(/\/$/, ""),
  };
  return {
    "@context": "https://schema.org",
    "@type": "BlogPosting",
    "@id": `${canonical}#article`,
    headline: post.data.headline ?? post.data.title,
    description: post.data.description,
    url: canonical,
    mainEntityOfPage: canonical,
    datePublished: post.data.published.toISOString(),
    dateModified: (post.data.updated ?? post.data.published).toISOString(),
    image: absolute(postImage(post.id), url),
    keywords: post.data.keyword,
    inLanguage: "en",
    author: person,
    publisher: person,
    isPartOf: {
      "@type": "Blog",
      "@id": absolute("/blog/#blog", url),
      name: blogTitle,
      url: absolute("/blog/", url),
    },
    about: { "@id": absolute("/#software", url) },
  };
}

export function collectionJsonLd(posts: readonly Post[], url: string) {
  return {
    "@context": "https://schema.org",
    "@type": "CollectionPage",
    "@id": absolute("/blog/#blog", url),
    url: absolute("/blog/", url),
    name: blogTitle,
    description: blogDescription,
    isPartOf: { "@id": absolute("/#website", url) },
    about: { "@id": absolute("/#software", url) },
    mainEntity: {
      "@type": "ItemList",
      itemListElement: posts.map((post, index) => ({
        "@type": "ListItem",
        position: index + 1,
        url: absolute(postHref(post.id), url),
        name: post.data.title,
      })),
    },
  };
}

export function llmsBlog(posts: readonly Post[], url: string): string {
  return `\n## Blog\n\n${posts.map((post) => `- [${post.data.title}](${absolute(postHref(post.id), url)}): ${post.data.description}`).join("\n")}\n`;
}

export const demoProps: Record<string, readonly string[]> = {
  DemoFrame: ["caption", "wide"],
  MonitorDemo: ["caption"],
  QueueDemo: ["caption"],
  FlowDemo: ["caption"],
  AgentDemo: ["caption"],
  CommandDemo: ["caption", "command"],
  RunSummary: ["caption"],
  Figure: ["caption"],
};

export type CommandName = "init" | "doctor" | "status" | "run" | "test" | "logs";

/** Validate against the recordings actually loaded, including at the Astro prop boundary. */
export function recordedCommand<T extends { name: string }>(
  commands: readonly T[],
  name: string,
): T {
  const cmd = commands.find((entry) => entry.name === name);
  if (!cmd)
    throw new Error(
      `CommandDemo: unknown command '${name}' (allowed: ${commands.map((entry) => entry.name).join(" | ")})`,
    );
  return cmd;
}
