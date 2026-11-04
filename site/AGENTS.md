# rr

Instructions for coding agents working in this repository. Keep this file under about 200 lines: it is loaded at the start of every session. Put detail that matters only for one area in a doc and link it from Deeper context.

## Overview

The static site for rr at rr.steamfield.io: a landing page whose demos replay real rr output, and docs rendered from the parent repository's `docs/` folder. Astro (static), React islands, Tailwind 4, Bun. It lives in `site/` inside the rr Go repository and is scaffolded from the stacks `astro-product-site` stack.

## Commands

Run these from `site/`. They are the only supported way to build, test, and run the project; if a command here is wrong, fix it here in the same change.

| Command | What it does |
| --- | --- |
| `make install` | Install dependencies (frozen lockfile in CI) and, outside CI, Playwright's Chromium |
| `make dev` | Dev server on `PORT_WEB` (4321) |
| `make lint` | Biome (TS, JS, CSS, JSON) and Prettier (`.astro`) |
| `make format` | Apply Biome and Prettier fixes |
| `make check` | `astro check` type-check |
| `make test` | Unit and component tests (vitest) |
| `bunx vitest run src/lib/monitor/render.test.ts` | One test file |
| `make build` | Static build into `dist/`, then `tests/build.test.ts` over it |
| `make e2e` | Playwright and axe against the production build (`PORT_E2E`, 4322) |
| `make deploy-check` | Serve `dist/` through `wrangler pages dev`; checks headers, canonical URLs, 404 |
| `make brand` | Render the brand pack's WebP files and manifest from `public/brand/*.svg` |
| `make capture` | Re-record the hero run (needs `CAPTURE_HOSTS` up; default OpenData) |
| `make capture-commands` | Re-record the Commands section against the rr repo's hosts |
| `make capture-agent` | Re-record the Agent session's rr runs in a temp worktree |
| `stacks verify` | The definition of done (install through serve) |

Each target also has a `bun run <target>` alias in `package.json` (`bun run verify` is `stacks verify`). `install` has none: bun runs an `install` script as a lifecycle hook of `bun install`, which `make install` calls.

The parent repository's Go test `internal/monitor/site_golden_test.go` pins `fixtures/monitor-golden.json`; after an intended TUI rendering change run `RR_WRITE_SITE_GOLDEN=1 go test ./internal/monitor -run TestSiteGolden` from the repo root.

## Architecture map

| Path | Owns |
| --- | --- |
| `src/config.ts` | All landing copy (from the README), install and plugin commands, docs groups, `site.islands` |
| `src/pages/index.astro` | The landing page: section order and which island goes where |
| `src/components/landing/` | Static sections (`sections.tsx`) and islands: `MonitorStage`, `FlowDiagram`, `QueueBoard`, `CommandTabs`, `TerminalPlayer`, `InstallTabs` |
| `src/components/landing/monitor/` | `MonitorCard` and the SVG dot renderer for braille sparklines |
| `src/lib/monitor/` | `render.ts` (port of the TUI's rendering), `timeline.ts` (snapshots to frames) |
| `src/lib/queue.ts` | The Queue board's model of rr's host choice and wait message |
| `src/lib/flow.ts` | The How it works diagram's simulated timeline (same lock rules, labeled illustrative) |
| `src/lib/use-playback.ts` | Clock for the Monitor stage (visibility, reduced motion, pause/replay, 50ms ticks) |
| `@steamfield/ui` | Shared library: the terminal player's clock (`useAgentDemo`), the flow diagram's clock (`usePlayback`), command and install tabs, copy buttons, scroll reveals, the TOC highlight, the docs Markdown plugins and the `.md` exports; `src/styles/sf-theme.css` maps rr's tokens onto its `--sf-*` variables |
| `src/data/` | Build-time loaders from `fixtures/` to island props |
| `src/components/Logo.astro`, `public/brand/`, `scripts/brand.ts` | The mark (⣾, rr's spinner frame) in header and footer. `public/brand/` holds the source SVGs (icon tile, marks, horizontal and stacked lockups, light and dark); `make brand` renders their WebP sizes, the social cards and `public/site.webmanifest`. `public/favicon.svg` is a copy of `rr-icon.svg`. `design/logo/` keeps the concept exploration and its `preview.html` |
| `fixtures/` | Scrubbed real rr recordings and the monitor golden file; `fixtures/raw/` is gitignored |
| `scripts/capture-*.sh`, `scripts/scrub-*.mjs` | Recording and scrubbing |
| `src/lib/docs-source.ts` | Which `../docs/*.md` files are pages (`RR_DOCS_DIR` overrides the folder) |
| `src/lib/docs.ts`, `src/lib/markdown.ts` | Docs rules, exports, and Sätteri plugins (incl. GitHub-link rewriting) |
| `src/content-drift.test.ts` | Page copy and recordings checked against README and Go source |
| `docs/DEVELOPMENT.md`, `docs/PRODUCT.md` | How it fits together; why the page is shaped this way |

## Agent harness

Claude Code reads `CLAUDE.md`, which imports this file. Hooks in `.claude/` block destructive commands and secret-shaped writes, lint each written file and format changed files on stop (`.claude/README.md`).

## Conventions

- Prefer the project's existing patterns and dependencies over new ones. Add a dependency only when nothing in the project already covers the need.
- Keep changes scoped to the task. Mention unrelated problems you notice instead of fixing them in the same change.
- Dark only, rr's TUI palette. Style with token utilities, never raw hex or `dark:` variants; new colors are tokens in `global.css` and must pass `src/styles/tokens.test.ts`.
- Demos replay recordings. Never hand-write rr output; re-record with the capture targets. Hand-written agent prose and the simulated How it works diagram are labeled as illustrative on the page.
- Every island server-renders a useful state (no-JS and reduced motion) and is listed in `site.islands`.
- Docs content belongs to the parent repo's `docs/`; write links GitHub-style (`configuration.md#lock`).

## Guardrails

- Destructive commands, credential-shaped writes and real `.env` writes are guarded by native hooks when this project targets Claude Code, Codex, OpenCode or Cursor. A block means ask the user; never rephrase a command or use another tool to bypass it.
- Store credentials in environment variables or a secrets manager. Write variable names and placeholders in `.env.example`; ask the user to set real values.
- Load the selected agent's applicable project skills or rules before changing their area.
- Treat lint diagnostics as work to resolve. Run the project check command before finishing; instructions and hooks do not replace tests.
- Capture scripts run real test suites on the user's machines (OpenData's full suite takes minutes on three hosts). Ask before running them.

## Testing

- Behavior-focused tests against real dependencies where practical. Fake only what crosses the network to third parties.
- A bug fix starts with a test that reproduces the bug.
- Weakening or skipping a test to get green is a failure, not a fix.

## Error handling

- Never swallow an error. Handle it, or add context and pass it on.
- Log an error once, at the boundary that handles it.

## Debugging

- If a fix hasn't worked after two attempts with no new diagnostic step in between, stop. Write down what you learned, list two or three other root-cause hypotheses, and ask which to pursue. Running a new diagnostic between attempts resets the count.
- Read logs and error output before guessing.
- rr writes a parallel run's `--pretty` output and per-task logs when the run ends; an empty output file mid-run is not a hang.

## Analytics

`src/lib/analytics.ts` is the only `posthog-js` import; it does nothing while `PUBLIC_POSTHOG_KEY` is empty (dev, tests, CI). Events live in `src/lib/events.ts` (`install_copied {method}`, `plugin_copied`, `section_viewed {id}`, `command_tab_opened {name}`), named `object_verb` in the past tense with flat scalar properties; track from islands or `src/scripts/site.ts`, never docs. Consent: cookieless (`cookieless_mode: "always"`), no banner. Never send typed text, emails or tokens.

## Deploy

Cloudflare Pages project `rr` (`rr-aqz.pages.dev`), production at rr.steamfield.io through a proxied CNAME in the steamfield.io zone, deployed by Cloudflare's Git integration from `main`, with a preview for every branch: root directory `site`, build command `bun install --frozen-lockfile && make build`, output `dist`, `BUN_VERSION` and `NODE_VERSION` build variables matching `package.json` and `.node-version`. Build watch paths are `site/*`, `docs/*`, `CHANGELOG.md` and `README.md`, the inputs that change the built pages; the Go paths in `.github/workflows/site.yml` only feed drift tests. `PUBLIC_POSTHOG_KEY` and `PUBLIC_POSTHOG_HOST` are Pages build variables. `make deploy-check` verifies headers and URLs locally; `make deploy-preview BRANCH=…` and `make deploy` upload from a machine with `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`. Headers live in `public/_headers`; add `public/_redirects` only for moved pages. `.github/workflows/site.yml` runs the checks (not a required check).

## Build workarounds

Things in the tree that exist to get a build through, not because they are right. Each one says when it can be removed. Re-check them when upgrading the tools involved.

| Where | What and why | Remove when |
| --- | --- | --- |
| `package.json` | `typescript@^6`: `astro check` refuses TypeScript 7 | `@astrojs/check` (or its replacement) supports TypeScript 7 |
| Component tests | Stub `IntersectionObserver`: happy-dom never reports intersection, so demos would never play | happy-dom reports intersection |

## Deferred

- Light theme: the TUI is dark only.
- A blog or changelog pages (the README's changelog link covers it).
- Contributor docs (ARCHITECTURE, DEVELOPMENT, releasing) stay GitHub-only.
- A second hero capture with a host dropping offline mid-run, and a green-run variant of the hero.

## Pending user actions

- Create a PostHog project, set `PUBLIC_POSTHOG_KEY` (and host) as Pages build variables, and enable cookieless tracking in its settings, or its events are ignored.
- Review the scrub summary and fixture diffs before committing recordings.

## Deeper context

Read these only when the task needs them.

| Need | Read |
| --- | --- |
| How the site fits together, docs pipeline, recordings | `docs/DEVELOPMENT.md` |
| Why the page is shaped this way, success metrics | `docs/PRODUCT.md` |
| Hooks and Claude settings | `.claude/README.md` |
