---
paths:
  - "src/content/**"
  - "src/lib/docs.ts"
  - "src/lib/markdown.ts"
  - "src/layouts/DocsLayout.astro"
  - "src/pages/docs/**"
---

Read "Docs pages" and "Markdown processing" in [the development guide](../../../docs/DEVELOPMENT.md) before changing docs content or the docs pipeline.

- A new page is one Markdown file with `title`, `description` and `group`. Don't list pages anywhere else; navigation, exports and the sitemap derive from the collection.
- Use the plain Markdown conventions (`> [!NOTE]` callouts, `title="..."` on fences), not MDX components.
- Link with the full route and trailing slash. `make build` fails on a broken link or anchor.
