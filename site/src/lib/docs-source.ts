// Where the docs pages come from. The site renders the rr repository's own docs/
// folder, so GitHub and the site read one copy. Every top-level .md file there is
// a page except the contributor docs below; a new user doc needs only frontmatter.
// RR_DOCS_DIR points the build somewhere else (the build test's probe copy).

/** Contributor docs that stay on GitHub only. */
export const contributorDocs = ["ARCHITECTURE.md", "DEVELOPMENT.md", "releasing.md"];

export const docsDir = process.env.RR_DOCS_DIR ?? "../docs";

/** Glob patterns for Astro's glob loader, relative to docsDir. */
export const docsPattern = ["*.md", ...contributorDocs.map((file) => `!${file}`)];

/** Where links to repository files that aren't site pages go. */
export const repoBlob = "https://github.com/rileyhilliard/rr/blob/main";

/** The page id Astro's glob loader gives a docs file: its name, lowercased, without .md. */
export const docId = (file: string) => file.replace(/\.md$/i, "").toLowerCase();

/** True when a docs/ file name is a site page rather than a contributor doc. */
export const isSitePage = (file: string) =>
  file.endsWith(".md") && !file.includes("/") && !contributorDocs.includes(file);
