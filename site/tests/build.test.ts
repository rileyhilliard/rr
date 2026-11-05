// Acceptance tests over the real build in dist/. `make build` runs them after
// `astro build`. The page list comes from the docs source (src/lib/docs-source.ts:
// the repository's docs/ folder), not from the build.
import { spawnSync } from "node:child_process";
import {
  cpSync,
  existsSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { Window } from "happy-dom";
import { type MdastNode, mdxToMdast } from "satteri";
import ts from "typescript";
import { describe, expect, it } from "vitest";
import { site } from "../src/config";
import { HERO_RUN } from "../src/data/hero-run";
import { demoProps, isDraft } from "../src/lib/blog";
import { docId, docsDir, isSitePage } from "../src/lib/docs-source";

const parser = new new Window().DOMParser();
const read = (root: string, path: string) => readFileSync(join(root, path), "utf8");
// Parsed once per file: the link check looks pages up for every anchor.
type Parsed = ReturnType<typeof parser.parseFromString>;
const parsed = new Map<string, Parsed>();
const page = (root: string, path: string): Parsed => {
  const key = join(root, path);
  const cached = parsed.get(key);
  if (cached) return cached;
  const doc = parser.parseFromString(read(root, path), "text/html");
  parsed.set(key, doc);
  return doc;
};
const absolute = (path: string) => new URL(path, site.url).href;
const isWebp = (file: Buffer) =>
  file.subarray(0, 4).toString() === "RIFF" && file.subarray(8, 12).toString() === "WEBP";
const pagePath = (id: string) => `docs/${id}/index.html`;

function sourceDocs(base = docsDir) {
  return readdirSync(base, { encoding: "utf8" })
    .filter(isSitePage)
    .map((file) => {
      const source = readFileSync(join(base, file), "utf8");
      const title = /^title:\s*["']?(.+?)["']?\s*$/m.exec(source);
      return { id: docId(file), title: title?.[1] ?? "", source };
    });
}

function htmlFiles(root: string) {
  return readdirSync(root, { recursive: true, encoding: "utf8" }).filter((file) =>
    file.endsWith(".html"),
  );
}

function islandNames(root: string, path: string) {
  return [...page(root, path).querySelectorAll("astro-island")].map(
    (island) => JSON.parse(island.getAttribute("opts") ?? "{}").name as string,
  );
}

const docs = sourceDocs();
const posts = (existsSync("src/content/blog") ? readdirSync("src/content/blog") : [])
  .filter((file) => file.endsWith(".mdx"))
  .map((file) => ({ id: file.slice(0, -4), source: read("src/content/blog", file) }));
const livePosts = posts.filter((post) => !isDraft(post.source));
const jsonLd = (doc: Parsed) =>
  [...doc.querySelectorAll('script[type="application/ld+json"]')].map((script) =>
    JSON.parse(script.textContent ?? ""),
  );
function noAbsentValues(value: unknown) {
  expect(value).not.toBeNull();
  expect(value).not.toBeUndefined();
  if (value && typeof value === "object")
    for (const child of Object.values(value)) noAbsentValues(child);
}

describe("docs pages", () => {
  it.each(docs)("$id renders its metadata, sidebar entry and exports", ({ id, title }) => {
    const doc = page("dist", pagePath(id));
    const href = `/docs/${id}/`;
    expect(doc.querySelector("h1")?.textContent).toBe(title);
    expect(doc.querySelector('meta[name="description"]')?.getAttribute("content")).toBeTruthy();
    expect(doc.querySelector('link[rel="canonical"]')?.getAttribute("href")).toBe(absolute(href));
    expect(doc.querySelector('meta[property="og:image"]')?.getAttribute("content")).toBe(
      absolute(`/og/docs/${id}.webp`),
    );
    expect(
      doc.querySelector('link[rel="alternate"][type="text/markdown"]')?.getAttribute("href"),
    ).toBe(`/docs/${id}.md`);
    expect(
      doc.querySelector('nav[aria-label="Docs"] a[aria-current="page"]')?.getAttribute("href"),
    ).toBe(href);
    expect(read("dist", `docs/${id}.md`).startsWith(`# ${title}\n`)).toBe(true);
    expect(read("dist", "llms.txt")).toContain(absolute(`/docs/${id}.md`));
    expect(read("dist", "llms-full.txt")).toContain(`# ${title}\n`);
    expect(read("dist", "sitemap-0.xml")).toContain(`<loc>${absolute(href)}</loc>`);
    expect(isWebp(readFileSync(`dist/og/docs/${id}.webp`))).toBe(true);
  });

  it("renders the Markdown conventions the docs use", () => {
    const pages = docs.map(({ id }) => page("dist", pagePath(id)));
    const all = (selector: string) => pages.flatMap((doc) => [...doc.querySelectorAll(selector)]);
    const source = docs.map((doc) => doc.source).join("\n");
    // Callouts and titled fences are checked where a page uses them; markdown.test.ts covers both.
    if (/^> \[!NOTE\]/m.test(source))
      expect(all('blockquote[role="note"][data-callout="note"]').length).toBeGreaterThan(0);
    const fenceTitle = /^```\w*[^\n]*title="([^"]+)"/m.exec(source)?.[1];
    if (fenceTitle)
      expect(
        all("figure[data-code-block] > figcaption[data-code-title]").map((el) => el.textContent),
      ).toContain(fenceTitle);
    expect(
      all('div[data-table-scroll][tabindex="0"][role="region"] > table').length,
    ).toBeGreaterThan(0);
    expect(all(":is(h2, h3) > a[data-heading-anchor][href^='#']").length).toBeGreaterThan(0);
    expect(
      all("figure[data-code-block] pre").every((pre) =>
        /--shiki-dark:/.test(pre.getAttribute("style") ?? ""),
      ),
    ).toBe(true);
  });

  it("lists every page on the docs home", () => {
    const home = page("dist", "docs/index.html");
    for (const { id } of docs) {
      expect(home.querySelector(`main a[href="/docs/${id}/"]`), id).not.toBeNull();
    }
  });
});

describe("the whole site", () => {
  it("has no broken internal links or anchors", () => {
    const broken: string[] = [];
    for (const file of htmlFiles("dist")) {
      const self = `/${file.replace(/index\.html$/, "")}`;
      for (const link of page("dist", file).querySelectorAll("a[href]")) {
        const href = link.getAttribute("href") ?? "";
        if (!href.startsWith("/") && !href.startsWith("#")) continue;
        if (href.startsWith("//")) continue;
        const [path = "", hash] = (href.startsWith("#") ? `${self}${href}` : href).split("#");
        const target = path.endsWith("/") ? `${path}index.html` : path;
        if (!existsSync(join("dist", target))) {
          broken.push(`${file}: ${href}`);
        } else if (hash && target.endsWith(".html")) {
          if (!page("dist", target).getElementById(decodeURIComponent(hash))) {
            broken.push(`${file}: ${href} (no #${hash})`);
          }
        }
      }
    }
    expect(broken).toEqual([]);
  });

  it("hydrates only site.islands on the landing page and blog articles", () => {
    expect([...new Set(islandNames("dist", "index.html"))].sort()).toEqual(
      [...site.islands].sort(),
    );
    for (const file of htmlFiles("dist")) {
      const names = islandNames("dist", file);
      if (file === "index.html" || /^blog\/[^/]+\/index\.html$/.test(file)) {
        for (const name of names) expect(site.islands, file).toContain(name);
      } else expect(names, file).toEqual([]);
    }
  });

  it("links icons and a manifest that exist in the build", () => {
    const home = page("dist", "index.html");
    const hrefs = ['link[rel="icon"]', 'link[rel="apple-touch-icon"]', 'link[rel="manifest"]'].map(
      (selector) => home.querySelector(selector)?.getAttribute("href") ?? "",
    );
    for (const href of hrefs) expect(existsSync(join("dist", href)), href).toBe(true);
    const manifest = JSON.parse(read("dist", "site.webmanifest")) as { icons: { src: string }[] };
    for (const { src } of manifest.icons) {
      expect(isWebp(readFileSync(join("dist", src))), src).toBe(true);
    }
  });

  it("renders the landing page from config, hiding empty sections", () => {
    const home = page("dist", "index.html");
    expect([...home.querySelectorAll("h1")].map((heading) => heading.textContent)).toEqual([
      site.tagline,
    ]);
    expect(home.querySelector("#hero-title p")?.textContent).toMatch(
      /^rr is a remote test runner\./,
    );
    const text = home.body.textContent ?? "";
    expect(text).toContain(site.install[0]?.command ?? "");
    for (const line of [...site.why.before.lines, ...site.why.after.lines]) {
      expect(text).toContain(line);
    }
    for (const command of site.plugin) expect(text).toContain(command);
    // The Monitor stage server-renders a frame with every captured host.
    for (const host of HERO_RUN.hosts) {
      expect(home.querySelector(`article[aria-label^="${host}:"]`), host).not.toBeNull();
    }
    for (const id of [
      "why",
      "queue",
      "faster",
      "fallback",
      "commands",
      "agent",
      "setup",
      "compare",
      "install",
    ]) {
      expect(home.getElementById(id)?.querySelector("h2"), id).not.toBeNull();
    }
    if (site.announcement) expect(text).toContain(site.announcement.label);
    if (site.version) expect(text).toContain(`v${site.version}`);
    expect(text).toContain(`${site.license} license`);
  });

  it("describes the homepage consistently in search and social metadata", () => {
    const home = page("dist", "index.html");
    const title = "Remote Test Runner for Coding Agents | rr";
    const description =
      "rr is an open-source remote test runner for coding agents. Send tests to your own machines over SSH, queue concurrent runs, and run shards across hosts.";
    const canonical = absolute("/");
    const image = absolute("/og/index.webp");
    expect(home.querySelector("title")?.textContent).toBe(title);
    expect(home.querySelector('meta[name="description"]')?.getAttribute("content")).toBe(
      description,
    );
    expect(home.querySelector('link[rel="canonical"]')?.getAttribute("href")).toBe(canonical);
    for (const [selector, content] of [
      ['meta[property="og:title"]', title],
      ['meta[property="og:description"]', description],
      ['meta[property="og:url"]', canonical],
      ['meta[property="og:image"]', image],
      ['meta[name="twitter:title"]', title],
      ['meta[name="twitter:description"]', description],
      ['meta[name="twitter:image"]', image],
    ] as const) {
      expect(home.querySelector(selector)?.getAttribute("content"), selector).toBe(content);
    }
    expect(isWebp(readFileSync("dist/og/index.webp"))).toBe(true);
  });

  it("publishes the exact home graph, blog graphs, and no structured data elsewhere", () => {
    for (const file of htmlFiles("dist")) {
      const graph = jsonLd(page("dist", file));
      noAbsentValues(graph);
      if (file === "index.html")
        expect(graph).toEqual([
          {
            "@context": "https://schema.org",
            "@type": "WebSite",
            "@id": absolute("/#website"),
            name: site.name,
            url: absolute("/"),
          },
          {
            "@context": "https://schema.org",
            "@type": "SoftwareApplication",
            "@id": absolute("/#software"),
            name: site.name,
            description: site.description,
            url: absolute("/"),
            applicationCategory: "DeveloperApplication",
            operatingSystem: "macOS, Linux, Windows (WSL)",
            license: `${site.repo}/blob/main/LICENSE`,
            sameAs: [site.repo],
          },
        ]);
      else if (file.startsWith("blog/"))
        expect(graph.map((item) => item["@type"])).toEqual([
          file === "blog/index.html" ? "CollectionPage" : "BlogPosting",
          "BreadcrumbList",
        ]);
      else expect(graph, file).toEqual([]);
    }
  });

  it("excludes the landing replay regions and blog terminal demos from search snippets", () => {
    const home = page("dist", "index.html");
    expect(home.querySelectorAll("[data-nosnippet]")).toHaveLength(3);
    expect(
      home.querySelector('section[aria-label="Pretty output"][data-nosnippet]'),
    ).not.toBeNull();
    expect(home.querySelector("#commands div[data-nosnippet] > astro-island")).not.toBeNull();
    expect(home.querySelector("#agent div[data-nosnippet] > astro-island")).not.toBeNull();
    for (const { id } of livePosts) {
      const doc = page("dist", `blog/${id}/index.html`);
      for (const island of doc.querySelectorAll("astro-island")) {
        const name = JSON.parse(island.getAttribute("opts") ?? "{}").name;
        if (["TerminalPlayer", "MonitorStage"].includes(name))
          expect(island.closest("[data-nosnippet]"), `${id}: ${name}`).not.toBeNull();
      }
    }
  });

  it("points every landing copy button at its own snippet", () => {
    const home = page("dist", "index.html");
    const buttons = [...home.querySelectorAll("[data-copy-command]")];
    const ids = buttons.map((button) => button.getAttribute("aria-controls") ?? "");
    expect(buttons.length).toBeGreaterThan(0);
    expect(new Set(ids).size).toBe(ids.length);
    for (const [i, button] of buttons.entries()) {
      const matches = home.querySelectorAll(`[id="${ids[i]}"]`);
      expect(matches, ids[i]).toHaveLength(1);
      const text = matches[0]?.textContent?.trim();
      const frame = button.closest("[data-code-block]");
      // Code frames copy their config example; command lines copy their command.
      const snippets = frame
        ? [site.fallback.config, site.setup.global, site.setup.project]
        : [...site.install.map((method) => method.command), ...site.plugin];
      expect(
        snippets.map((snippet) => snippet.trim()),
        ids[i],
      ).toContain(text);
    }
    const frames = home.querySelectorAll("[data-code-block] [data-copy-command]");
    expect(frames).toHaveLength(3);
  });

  it("serves robots.txt, the sitemap index and an unindexed 404 page", () => {
    const robots = read("dist", "robots.txt");
    expect(robots.trim().split(/\n+/)).toEqual([
      "User-agent: *",
      "Allow: /",
      `Sitemap: ${absolute("/sitemap-index.xml")}`,
    ]);
    expect(existsSync("dist/sitemap-index.xml")).toBe(true);
    const sitemap = read("dist", "sitemap-0.xml");
    expect(sitemap).toContain(`<loc>${absolute("/")}</loc>`);
    expect(sitemap).not.toMatch(/404|\.md<|\.webp<|\.txt</);
    for (const file of htmlFiles("dist").filter((file) => file.endsWith("index.html"))) {
      const directives = [...page("dist", file).querySelectorAll('meta[name="robots"]')].map(
        (meta) => meta.getAttribute("content"),
      );
      expect(directives, file).toEqual(["max-snippet:-1, max-image-preview:large"]);
    }
    const notFound = page("dist", "404.html");
    expect(notFound.querySelector("h1")?.textContent).toBe("Page not found");
    expect(notFound.querySelector('meta[name="robots"]')?.getAttribute("content")).toBe(
      "noindex, nofollow",
    );
  });
});

function demoErrors(source: string): string[] {
  const errors: string[] = [];
  const seen = new Set<string>();
  const bindings = new Map<string, string>();
  const tree = mdxToMdast(source);
  if ("children" in tree)
    for (const node of tree.children) {
      if (node.type !== "mdxjsEsm") continue;
      const module = ts.createSourceFile("post.ts", node.value, ts.ScriptTarget.Latest, true);
      for (const statement of module.statements) {
        if (!ts.isImportDeclaration(statement) || !ts.isStringLiteral(statement.moduleSpecifier))
          continue;
        const component = /(?:^|\/)components\/blog\/([^/]+)\.astro$/.exec(
          statement.moduleSpecifier.text,
        )?.[1];
        if (!component || !demoProps[component]) continue;
        const clause = statement.importClause;
        if (!clause?.name || clause.namedBindings)
          errors.push(`${component} must use a default import from its own file`);
        else bindings.set(clause.name.text, component);
      }
    }
  const visit = (node: MdastNode) => {
    if (node.type === "mdxJsxFlowElement" || node.type === "mdxJsxTextElement") {
      const component = node.name ? (bindings.get(node.name) ?? node.name) : undefined;
      const allowed = component ? demoProps[component] : undefined;
      if (allowed && component) {
        if (component !== "Figure") {
          if (seen.has(component)) errors.push(`${component} used more than once`);
          seen.add(component);
        }
        for (const attr of node.attributes) {
          if (attr.type !== "mdxJsxAttribute") errors.push(`${component} must use explicit props`);
          else if (!allowed.includes(attr.name))
            errors.push(`${component}.${attr.name} is not an allowed prop`);
        }
      }
    }
    if ("children" in node) for (const child of node.children) visit(child);
  };
  visit(tree);
  return errors;
}

describe("blog", () => {
  const rss = parser.parseFromString(read("dist", "blog/rss.xml"), "application/xml");
  const index = page("dist", "blog/index.html");
  const sitemap = readdirSync("dist")
    .filter((file) => /^sitemap.*\.xml$/.test(file))
    .map((file) => read("dist", file))
    .join("\n");

  it.each(livePosts)(
    "$id renders consistent article metadata and discovery links",
    ({ id, source }) => {
      const file = `blog/${id}/index.html`;
      expect(existsSync(join("dist", file))).toBe(true);
      const doc = page("dist", file);
      expect(doc.querySelectorAll("h1")).toHaveLength(1);
      if (/^> \[!NOTE\]/m.test(source))
        expect(doc.querySelector('blockquote[data-callout="note"]')).not.toBeNull();
      const fenceTitle = /^```\w*[^\n]*title="([^"]+)"/m.exec(source)?.[1];
      if (fenceTitle)
        expect(
          [...doc.querySelectorAll("[data-code-title]")].map((el) => el.textContent),
        ).toContain(fenceTitle);

      const title = doc.querySelector("title")?.textContent ?? "";
      expect(title.length).toBeLessThanOrEqual(60);
      const meta = (key: string) =>
        doc.querySelector(`meta[name="${key}"], meta[property="${key}"]`)?.getAttribute("content");
      expect(meta("og:title")).toBe(title);
      expect(meta("twitter:title")).toBe(title);
      const description = meta("description") ?? "";
      expect(description.length).toBeGreaterThanOrEqual(120);
      expect(description.length).toBeLessThanOrEqual(160);
      expect(meta("og:description")).toBe(description);
      expect(meta("twitter:description")).toBe(description);
      const url = absolute(`/blog/${id}/`);
      expect(doc.querySelector('link[rel="canonical"]')?.getAttribute("href")).toBe(url);
      expect(meta("og:type")).toBe("article");
      for (const key of ["article:published_time", "article:modified_time"]) {
        const value = meta(key) ?? "";
        expect(new Date(value).toISOString()).toBe(value);
      }
      const graph = jsonLd(doc);
      graph.forEach(noAbsentValues);
      const article = graph.find((item) => item["@type"] === "BlogPosting");
      const software = jsonLd(page("dist", "index.html")).find(
        (item) => item["@type"] === "SoftwareApplication",
      );
      expect(article).toMatchObject({
        "@id": `${url}#article`,
        url,
        mainEntityOfPage: url,
        datePublished: meta("article:published_time"),
        dateModified: meta("article:modified_time"),
        image: absolute(`/og/blog/${id}.webp`),
        about: { "@id": software["@id"] },
      });
      const crumbs = graph.find((item) => item["@type"] === "BreadcrumbList").itemListElement;
      expect(crumbs.map((item: { name: string }) => item.name)).toEqual([
        "Home",
        "Blog",
        title.replace(/ \| rr$/, ""),
      ]);
      expect(crumbs.at(-1)).not.toHaveProperty("item");
      expect(meta("og:image")).toBe(article.image);
      expect(isWebp(readFileSync(`dist/og/blog/${id}.webp`))).toBe(true);
      expect(sitemap).toContain(`<loc>${url}</loc>`);
      const item = [...rss.querySelectorAll("item")].find(
        (item) => item.querySelector("link")?.textContent === url,
      );
      expect(item?.querySelector("title")?.textContent).toBe(title.replace(/ \| rr$/, ""));
      expect(item?.querySelector("description")?.textContent).toBe(description);
      expect(new Date(item?.querySelector("pubDate")?.textContent ?? "").toISOString()).toBe(
        article.datePublished,
      );
      expect(read("dist", "llms.txt")).toContain(
        `- [${title.replace(/ \| rr$/, "")}](${url}): ${description}`,
      );
      for (const link of doc.querySelectorAll("#related-docs + ul a[href]"))
        expect(existsSync(join("dist", link.getAttribute("href") ?? "", "index.html"))).toBe(true);
      const ids = [...doc.querySelectorAll("[id]")].map((el) => el.id);
      expect(ids.filter((value, index) => ids.indexOf(value) !== index)).toEqual([]);
      expect(doc.querySelectorAll("[data-toc]")).toHaveLength(
        doc.querySelectorAll(".prose h2, .prose h3").length ? 1 : 0,
      );
    },
  );

  it("lists every post newest first, has collection and breadcrumb data, and publishes RSS everywhere", () => {
    expect(index.querySelectorAll("h1")).toHaveLength(1);
    const ids = [...index.querySelectorAll("[id]")].map((el) => el.id);
    expect(ids.filter((value, index) => ids.indexOf(value) !== index)).toEqual([]);
    const graph = jsonLd(index);
    graph.forEach(noAbsentValues);
    const collection = graph.find((item) => item["@type"] === "CollectionPage");
    expect(collection).toMatchObject({
      "@id": absolute("/blog/#blog"),
      url: absolute("/blog/"),
      isPartOf: { "@id": absolute("/#website") },
      about: { "@id": absolute("/#software") },
    });
    const expected = livePosts
      .map(({ id }) => ({
        id,
        published: jsonLd(page("dist", `blog/${id}/index.html`))[0].datePublished,
      }))
      .sort((a, b) => b.published.localeCompare(a.published) || a.id.localeCompare(b.id));
    expect(
      [...index.querySelectorAll("main h2 a")].map((link) => link.getAttribute("href")),
    ).toEqual(expected.map(({ id }) => `/blog/${id}/`));
    expect(collection.mainEntity.itemListElement.map((item: { url: string }) => item.url)).toEqual(
      expected.map(({ id }) => absolute(`/blog/${id}/`)),
    );
    expect(graph[1].itemListElement.at(-1)).not.toHaveProperty("item");
    expect(rss.querySelectorAll("item")).toHaveLength(livePosts.length);
    expect(sitemap).toContain(`<loc>${absolute("/blog/")}</loc>`);
    expect(isWebp(readFileSync("dist/og/blog.webp"))).toBe(true);
    for (const file of htmlFiles("dist"))
      expect(
        page("dist", file)
          .querySelector('link[rel="alternate"][type="application/rss+xml"]')
          ?.getAttribute("href"),
        file,
      ).toBe("/blog/rss.xml");
    for (const { id } of posts.filter((post) => !livePosts.includes(post))) {
      expect(existsSync(`dist/blog/${id}/index.html`)).toBe(false);
      for (const content of [
        sitemap,
        read("dist", "blog/rss.xml"),
        read("dist", "llms.txt"),
        read("dist", "blog/index.html"),
      ])
        expect(content).not.toContain(`/blog/${id}/`);
    }
  });

  it("checks props and repeated uses even when a demo import is aliased", () => {
    expect(
      demoErrors(
        'import Queue from "../../components/blog/QueueDemo.astro";\n\n<Queue caption="Queue" hosts={["other"]} />',
      ),
    ).toContain("QueueDemo.hosts is not an allowed prop");
    expect(
      demoErrors(
        'import Queue from "../../components/blog/QueueDemo.astro";\n\n<Queue caption="One" />\n\n<Queue caption="Two" />',
      ),
    ).toContain("QueueDemo used more than once");
    expect(
      demoErrors(
        'import Queue from "../../components/blog/QueueDemo.astro";\n\n<Queue caption="Valid > caption" />',
      ),
    ).toEqual([]);
  });

  it.each(posts)("$id uses only documented demo props, once per component", ({ id, source }) => {
    expect(demoErrors(source), id).toEqual([]);
  });
});

describe("adding a docs page", () => {
  it("is one file: navigation, pager, exports and sitemap pick it up", () => {
    const root = mkdtempSync(join(tmpdir(), "site-build-"));
    try {
      for (const entry of [
        "src",
        "public",
        "fixtures",
        "astro.config.mjs",
        "tsconfig.json",
        "package.json",
      ]) {
        cpSync(entry, join(root, entry), { recursive: true });
      }
      symlinkSync(resolve("node_modules"), join(root, "node_modules"), "dir");
      const docsCopy = join(root, "docs-source");
      cpSync(docsDir, docsCopy, { recursive: true });
      writeFileSync(
        join(docsCopy, "probe-page.md"),
        "---\ntitle: Probe page\ndescription: Checks that a new page is wired in.\ngroup: guides\norder: -1000\n---\n\n# Probe page\n\n## Probe heading\n\nProbe body.\n",
      );
      const built = spawnSync("bunx", ["--no-install", "astro", "build"], {
        cwd: root,
        encoding: "utf8",
        timeout: 150_000,
        env: { ...process.env, RR_DOCS_DIR: docsCopy },
      });
      expect(built.status, `${built.stdout}\n${built.stderr}`).toBe(0);
      const dist = join(root, "dist");
      const href = "/docs/probe-page/";
      const other = page(dist, pagePath(docs[0]?.id ?? ""));
      expect(other.querySelector(`nav[aria-label="Docs"] a[href="${href}"]`)).not.toBeNull();
      // order -1000 makes the probe its group's first page, so another page links back to it.
      const linksBack = docs.some(
        ({ id }) =>
          page(dist, pagePath(id)).querySelector(`a[rel="prev"][href="${href}"]`) !== null,
      );
      expect(linksBack).toBe(true);
      expect(page(dist, pagePath("probe-page")).querySelector("#probe-heading")).not.toBeNull();
      expect(read(dist, "docs/probe-page.md")).toContain("Probe body.");
      expect(read(dist, "llms.txt")).toContain(absolute("/docs/probe-page.md"));
      expect(read(dist, "sitemap-0.xml")).toContain(absolute(href));
      expect(existsSync(join(dist, "og/docs/probe-page.webp"))).toBe(true);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});

// The layout only calls initAnalytics, and only creates the consent store and
// banner, when the build has a key, so a keyless build (dev, CI, previews) bundles
// none of it. The banner's stylesheet ships either way, with the other library CSS.
describe("analytics in the build", () => {
  // Every JavaScript file a page loads or may load: its inline scripts, script tags
  // and island components, renderers and before-hydration scripts, then the imports
  // (static, and dynamic with any quote style) of each file they reach. Ported from
  // stacks-api's analytics build test.
  function scriptsReachedFrom(root: string, page: string): Map<string, string> {
    const html = read(root, page);
    const reached = new Map<string, string>();
    const pending: string[] = [];
    const follow = (source: string, folder: string) => {
      // A `//host/...` target is another origin; it isn't in the build.
      for (const m of source.matchAll(/["'`](\.\/|\/(?!\/))([^"'`]+\.js)["'`]/g)) {
        const target = m[2] ?? "";
        // A template such as posthog-js's `/static/${version}/${name}.js` is a URL
        // built at run time for its CDN scripts, not a file in the build.
        if (target.includes("${")) continue;
        pending.push(m[1] === "/" ? target : folder + target);
      }
    };
    const pageFolder = page.slice(0, page.lastIndexOf("/") + 1);
    const inline = html.matchAll(/<script(?![^>]*\ssrc=)[^>]*>([\s\S]*?)<\/script>/g);
    for (const [index, m] of Array.from(inline).entries()) {
      const source = m[1] ?? "";
      reached.set(`${page}#inline-${index}`, source);
      follow(source, pageFolder);
    }
    const entries =
      /(?:<script[^>]*\ssrc|\scomponent-url|\srenderer-url|\sbefore-hydration-url)="\/(?!\/)([^"]+\.js)"/g;
    pending.push(...Array.from(html.matchAll(entries), (m) => m[1] ?? ""));
    for (let path = pending.pop(); path !== undefined; path = pending.pop()) {
      if (reached.has(path)) continue;
      const source = read(root, path);
      reached.set(path, source);
      follow(source, path.slice(0, path.lastIndexOf("/") + 1));
    }
    return reached;
  }

  // The stylesheets a page links, by path, with their contents.
  function stylesheetsOf(root: string, page: string): Map<string, string> {
    const html = read(root, page);
    const links = html.matchAll(/<link[^>]*\srel="stylesheet"[^>]*\shref="\/([^"]+)"/g);
    return new Map(Array.from(links, (m) => m[1] ?? "").map((path) => [path, read(root, path)]));
  }

  // The paths, not the contents, so a failure prints something readable.
  const filesContaining = (reached: Map<string, string>, text: string) =>
    [...reached].filter(([, source]) => source.includes(text)).map(([path]) => path);

  // The default ingest host: in the analytics module and in posthog-js, nowhere else.
  const analyticsMarker = "us.i.posthog.com";
  // posthog-js's consent record prefix: in the SDK only, so finding it means the
  // scanner followed the module's dynamic import of posthog-js.
  const sdkMarker = "__ph_opt_in_out_";
  // The consent store's cookie name: in @steamfield/ui/consent only.
  const consentMarker = "sf-consent";
  // A rule from @steamfield/ui/consent's stylesheet, and a variable it reads (which
  // the site's theme also sets).
  const bannerStyleMarker = "--sf-consent-background";
  const isConsentStyle = (source: string) => /\[data-part=["']?consent["']?\]/.test(source);

  // A production build into a temporary directory with this PostHog key, so the
  // result doesn't depend on the environment `make build` ran in.
  function buildWithKey(key: string) {
    const out = mkdtempSync(join(tmpdir(), "rr-analytics-"));
    const built = spawnSync("bunx", ["--no-install", "astro", "build", "--outDir", out], {
      encoding: "utf8",
      timeout: 120_000,
      env: { ...process.env, PUBLIC_POSTHOG_KEY: key },
    });
    expect(built.status, `${built.stdout}\n${built.stderr}`).toBe(0);
    return out;
  }

  // The site value the layout passes to initAnalytics, as the minifier writes it.
  const siteValue = /site:\s*["'`]rr["'`]/;

  it("loads no analytics or consent code without a PostHog key", () => {
    const out = buildWithKey("");
    try {
      const reached = scriptsReachedFrom(out, "index.html");
      expect(reached.size).toBeGreaterThan(0);
      expect(filesContaining(reached, analyticsMarker)).toEqual([]);
      expect(filesContaining(reached, consentMarker)).toEqual([]);
    } finally {
      rmSync(out, { recursive: true, force: true });
    }
  }, 150_000);

  it("loads the key once, posthog-js, the banner and its styles when the build has a key", () => {
    // Not a credential: any non-empty value turns analytics on for the build.
    const key = "rr-build-test-key";
    const out = buildWithKey(key);
    try {
      const reached = scriptsReachedFrom(out, "index.html");
      expect(filesContaining(reached, key)).toHaveLength(1);
      expect(filesContaining(reached, sdkMarker)).toHaveLength(1);
      expect(filesContaining(reached, consentMarker)).not.toEqual([]);
      const sources = [...reached.values()];
      expect(sources.some((source) => siteValue.test(source))).toBe(true);
      // The banner's text, which says what Accept turns on.
      expect(
        sources.some((source) =>
          source.includes(
            "The rr site counts visits and clicks with PostHog. Accept lets it set cookies so repeat visits count once, and turns on session recordings and heatmaps.",
          ),
        ),
      ).toBe(true);
      expect(sources.some((source) => /domain:\s*["'`]steamfield\.io["'`]/.test(source))).toBe(
        true,
      );
      const styles = [...stylesheetsOf(out, "index.html").values()];
      expect(styles.some((source) => source.includes(bannerStyleMarker))).toBe(true);
      expect(styles.some(isConsentStyle)).toBe(true);
    } finally {
      rmSync(out, { recursive: true, force: true });
    }
  }, 150_000);

  it("links the shared privacy page from the footer", () => {
    expect(
      page("dist", "index.html").querySelector('footer a[href="https://steamfield.io/privacy"]'),
    ).not.toBeNull();
  });
});
