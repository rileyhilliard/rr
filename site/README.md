# rr

The site for [rr](https://github.com/rileyhilliard/rr), the CLI that runs your test suite on spare machines instead of your laptop. It's published at [rr.steamfield.io](https://rr.steamfield.io).

A static [Astro](https://astro.build) site: a landing page at `/` whose demos replay real rr output, and documentation under `/docs/` rendered from the repository's own `docs/` folder. Every docs page is also served as Markdown, and `/llms.txt` lists them for coding agents. [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) explains how it fits together; [docs/PRODUCT.md](docs/PRODUCT.md) explains why the page is shaped the way it is.

## Quickstart

Requires [bun](https://bun.sh), Node and make.

```bash
make install
make dev        # http://localhost:4321
```

## Adding a docs page

Add a Markdown file to the repository's `docs/` folder with `title`, `description` and `group` in its frontmatter, and link to other pages the GitHub way (`configuration.md#lock`). The sidebar, pager, docs home, sitemap, social card, Markdown export and `llms.txt` pick it up on the next build. Contributor docs that shouldn't be pages are listed in `src/lib/docs-source.ts`.

## Re-recording the demos

The landing page's terminals replay recordings, so they go stale when rr's output changes. With your hosts up, run `make capture` (the hero run), `make capture-commands` or `make capture-agent`, read the scrub summary they print, and review the fixture diff before committing.

## Commands

| Command         | What it does                                       |
| --------------- | -------------------------------------------------- |
| `make dev`      | Dev server                                         |
| `make lint`     | Biome and Prettier checks                          |
| `make format`   | Apply formatting and safe fixes                    |
| `make check`    | Type-check pages, components and content           |
| `make test`     | Unit and component tests                           |
| `make build`    | Production build into `dist/`, then its acceptance tests |
| `make e2e`      | Browser and accessibility tests                    |
| `make preview`  | Serve the production build                         |
| `make deploy-check` | Serve `dist/` through `wrangler pages dev` and check headers and URLs |
| `make capture`, `capture-commands`, `capture-agent` | Re-record the demos against real hosts |

## License

MIT, like rr. Copyright (c) 2026 Riley Hilliard
