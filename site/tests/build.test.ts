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
import { describe, expect, it } from "vitest";
import { site } from "../src/config";
import { HERO_RUN } from "../src/data/hero-run";
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

  it("hydrates only the islands site.islands lists, and none on docs pages", () => {
    expect([...new Set(islandNames("dist", "index.html"))].sort()).toEqual(
      [...site.islands].sort(),
    );
    for (const file of htmlFiles("dist").filter((file) => file.startsWith("docs"))) {
      expect(islandNames("dist", file), file).toEqual([]);
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
    expect(home.querySelector("h1")?.textContent).toBe(site.tagline);
    expect(home.querySelector('meta[property="og:image"]')?.getAttribute("content")).toBe(
      absolute("/og/index.webp"),
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
    expect(isWebp(readFileSync("dist/og/index.webp"))).toBe(true);
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
    expect(read("dist", "robots.txt")).toContain(`Sitemap: ${absolute("/sitemap-index.xml")}`);
    expect(existsSync("dist/sitemap-index.xml")).toBe(true);
    expect(read("dist", "sitemap-0.xml")).not.toMatch(/404|\.md<|\.webp<|\.txt</);
    const notFound = page("dist", "404.html");
    expect(notFound.querySelector("h1")?.textContent).toBe("Page not found");
    expect(notFound.querySelector('meta[name="robots"]')?.getAttribute("content")).toBe(
      "noindex, nofollow",
    );
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
