# Developing the site

## Where things live

| Path | Owns |
| --- | --- |
| `src/config.ts` | Product name, tagline, URL, install and plugin commands, the landing copy and tables (from the README), links, license, the docs groups and the `islands` list. The version comes from `../CHANGELOG.md` at build time |
| `../docs/` | Docs pages: the rr repository's own `docs/` folder, validated by `src/content/schema.ts`. `src/lib/docs-source.ts` says which files are pages |
| `src/data/` | Build-time loaders that turn `fixtures/` recordings into island props (hero run, commands, agent session) |
| `fixtures/` | Real rr output recorded by `scripts/capture-*.sh`, scrubbed, plus `monitor-golden.json` (the Go TUI's render output for fixed inputs) |
| `src/lib/monitor/` | A port of `rr monitor`'s rendering (`render.ts`) and the snapshot timeline (`timeline.ts`) |
| `src/lib/docs.ts` | Pure docs rules: sidebar order, pager, URLs, the Markdown export and both llms files |
| `src/lib/collection.ts` | The only `astro:content` import outside routes |
| `src/lib/markdown.ts` | Docs Markdown conventions as Sätteri hast plugins plus one Shiki transformer |
| `src/lib/og.ts` | The social card layout and its fonts |
| `src/components/landing/` | Landing sections (static React) and the islands: `MonitorStage`, `QueueBoard`, `CommandTabs`, `TerminalPlayer`, `InstallTabs` |
| `src/scripts/site.ts` | Copy buttons, section-view and command-tab analytics, copy page and the TOC highlight |
| `src/layouts/` | `BaseLayout` (head, header, footer) and `DocsLayout` (sidebar, TOC, pager) |

## Docs pages

The pages are the rr repository's `docs/*.md`, so GitHub and the site read one copy. Every top-level file there is a page except the contributor docs listed in `src/lib/docs-source.ts` (ARCHITECTURE, DEVELOPMENT, releasing). `RR_DOCS_DIR` points a build at another folder; the build test uses it for its probe page.

Frontmatter: `title`, `description` (at most 160 characters), `group` (an id from `docsGroups`), optional `order` (default 100, ties sort by title) and `sidebarLabel`. GitHub shows it as a small table at the top of the file. `docs/ssh-setup.md` is served at `/docs/ssh-setup/`, exported at `/docs/ssh-setup.md` and drawn at `/og/docs/ssh-setup.webp`; ids are lowercased (`MIGRATION.md` is `/docs/migration/`). Nothing else lists pages by hand.

The files are written for GitHub: each starts with its own `# Title`, and links are relative file paths. `dropTitle` removes the h1 (the layout renders the frontmatter title), and `docsLinks` rewrites `ssh-setup.md#keys` to `/docs/ssh-setup/#keys` and any other repository path to its GitHub URL. The Markdown exports and `llms-full.txt` get the same treatment through `siteMarkdown` in `src/lib/docs.ts`.

Markdown conventions, all plain Markdown so pages read the same on GitHub and to agents:

- Callouts: `> [!NOTE]`, `> [!TIP]`, `> [!IMPORTANT]`, `> [!WARNING]`, `> [!CAUTION]`.
- Titled code blocks: `title="file.ts"` after the fence's language.
- `##` and `###` headings feed the table of contents and get anchor links.
- Tables scroll sideways inside their own region.

In the docs, link between pages the GitHub way (`configuration.md#lock`); the plugin turns it into the route. The build test fails on a link or `#anchor` that doesn't resolve.

## Markdown processing

Astro's default processor is Sätteri, which doesn't run remark or rehype plugins. The plugins in `src/lib/markdown.ts` use Sätteri's own API (`@astrojs/markdown-satteri`): objects with a tag `filter` and a `visit(node, ctx)` function. Shiki highlights code before hast plugins run and drops the fence's info string, so `codeTitle` (a Shiki transformer) copies `title="..."` onto the `<pre>` as `data-title`, and `codeFrames` turns it into a caption. `markdown.test.ts` renders through the same processor.

## Islands and scripts

Landing sections are React components rendered to HTML at build time. Only components named in `site.islands` may take a `client:` directive, and only on the landing page; the build test compares the hydrated islands with that list and fails on any island in `/docs/`. Prefer `client:visible`. Interactive behaviour shared by every page (copy buttons, analytics, TOC) goes in `src/scripts/site.ts` as plain DOM code that does nothing when its markup is absent.

Every island server-renders a useful state: the Monitor stage its busiest frame, the Queue board every host locked with a run waiting, the terminals their whole transcript, the command tabs every panel (CSS `:target` picks one, so `#cmd-doctor` works without JavaScript). `src/lib/use-playback.ts` is the shared clock: demos advance only while on screen, in a visible tab, with motion allowed and not paused.

## Demo recordings

The demos replay real rr output, never hand-written transcripts:

| Make target | Records | Into |
| --- | --- | --- |
| `make capture` | A parallel `rr test` in `CAPTURE_PROJECT` (OpenData by default) while `rr monitor --once --json` samples `CAPTURE_HOSTS`, then a `--pretty` run | `fixtures/monitor.jsonl`, `run.jsonl`, `run.pretty.txt` |
| `make capture-commands` | Each Commands tab against this repo's hosts (`rr init` under a temporary HOME) | `fixtures/commands/` |
| `make capture-agent` | In a temp worktree with `fixtures/agent/break.patch` applied: the failing filtered run, the passing one, then `rr test` | `fixtures/agent/` |

Each needs its hosts up and refuses a run that fell back to local. Raw output lands in `fixtures/raw/` (gitignored); the scrub step (`scripts/scrub-lib.mjs`) replaces the home directory, user and machine names, drops lock holders and failed tasks' `output_tail`, and prints what it replaced. Read that summary and the diff before committing. `src/content-drift.test.ts` checks the recordings and the page copy against the README and the Go source.

The monitor cards are drawn by `src/lib/monitor/render.ts`, a port of the TUI's braille sparklines, bars, thresholds and formatters. `internal/monitor/site_golden_test.go` writes the TUI's output for fixed inputs to `fixtures/monitor-golden.json` (`RR_WRITE_SITE_GOLDEN=1`) and fails when it drifts; `render.test.ts` holds the port to the same file. The site's fonts have no braille glyphs, so sparklines are SVG dot grids decoded from those characters.

## Theme

Dark only, like the rr TUI, whose palette (`internal/ui/colors.go`) the tokens reuse. There is no theme script or toggle. Components use the token utilities (`bg-canvas`, `bg-panel`, `text-ink`, `text-ink-2`, `text-muted`, `border-line`, `border-line-strong`, and the meaning colors `accent`, `info`, `success`, `waiting`, `error`, `sync`); `faint` is below AA and only for decoration. `src/styles/tokens.test.ts` checks every text and control pair for WCAG contrast, and the browser step runs axe with and without reduced motion.

Type: Instrument Sans for text, Martian Mono for code, transcripts and the monitor. The "rr" wordmark is TeX Gyre Heros Bold at -0.03em tracking, the same setting as Steamfield's wordmark (`../steamfield/brand/`, GUST Font License), outlined to a path, in `Wordmark.astro` and the `public/brand/` lockups, so no font loads for it; re-outline with fontTools if it changes.

Page rhythm: every landing section is a `Section` (`sections.tsx`); `band` puts it on the panel surface edge to edge, and bands alternate with the canvas so each section reads as its own block. The TUI gradient (`--gradient-brand`) appears only as a 2px rule: under the wordmark, along the hero window (`.term-hero`) and above the install band. The hero's `.dot-field` is the monitor's braille texture.

Motion, all in `global.css` and off under reduced motion, borrows OpenData's landing vocabulary. A `data-motion` role names how an element enters (`lift-sm`, `lift-md`, `lift-lg`, `lift-card`, `blur-in-soft`, `scale-in-media`: offset, duration, and the `--ease-organic` curves), and `--motion-delay` places it in its section's sequence. Children of `[data-stagger]` get their delay from their position (`--stagger-from` plus `--stagger` times `--stagger-gap`; these inherit, so a nested list sets its own). Each `Section` is a `[data-reveal]` root: `setupReveals` in `src/scripts/site.ts` marks `html.js`, adds `.is-visible` once when the section's top passes 75% of the viewport, and `.is-settled` when its transitions end so hover transitions aren't delayed; without the script nothing is hidden. The heading enters at 0ms, the lede at 120ms and the content from 240ms; wrap an island in a `data-motion` element, since `astro-island` is `display: contents` and can't be transformed. `.hero-in` plays the same roles as keyframes on load, the h1 included. The h1 is the LCP element, so it starts at 0ms; check LCP in Lighthouse after changing its entrance. `.grow-x` chart bars grow from the left, `.lift` is the hover lift, `.flash` marks a queue row that changed hands. The browser test scrolls the page through and waits for animations before running axe.

## Social cards

`/og/[...slug].webp` renders `ogCard` with satori, with the brand lockup from `public/brand/` as an image, and converts it to WebP with sharp. Fonts come from the installed `@fontsource/instrument-sans` package as WOFF; satori can't read WOFF2 and lays out flexbox only. To use one static image instead, put a 1200x630 WebP in `public/`, delete the route and `og.ts`, point the layouts' `image` at it and update the build test.

## Search

Search isn't part of this site yet. `BaseLayout` has an empty `search` slot in the header for a search component, and docs content sits in `<article>` inside `<main>` so a static indexer can scope to it.

## Running servers

Dev and preview pass `--ignore-lock` so they stay in the foreground under an agent. Stop an Astro-managed background instance with `bunx astro dev stop` or `bunx astro preview stop`. The browser tests serve `dist/` with `astro preview` on `PORT_E2E`.
