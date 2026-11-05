---
paths:
  - "src/components/**"
  - "src/layouts/**"
  - "src/pages/**"
  - "src/scripts/**"
  - "src/styles/**"
---

Read "Islands and scripts" and "Theme" in [the development guide](../../../docs/DEVELOPMENT.md) before changing pages, components or styles.

- Only components named in `site.islands` take a `client:` directive, and only on the landing page and in the `src/components/blog/` demo wrappers. Docs pages and the blog index ship no island; shared behaviour goes in `src/scripts/site.ts`.
- Style with the token utilities, never `dark:` variants, so the no-JavaScript theme fallback keeps working.
- Each landing section reads `src/config.ts` and renders nothing when its value is empty. Keep a colocated test for each component.
