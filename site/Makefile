# rr site commands.
#
# Adapt the recipes, but keep the target names: `stacks verify` calls install,
# lint, check, test, build, e2e and dev, and CI should call the same targets.

PORT_WEB ?= 4321
PORT_E2E ?= 4322
export PORT_WEB PORT_E2E

.DEFAULT_GOAL := help
.PHONY: help install dev lint format check test coverage build preview e2e clean brand capture capture-commands capture-agent deploy-check deploy-preview deploy

help: ## List targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-10s %s\n", $$1, $$2}'

# Outside CI this also installs git hooks (when configured) and the Chromium
# build the browser tests use; CI installs Chromium in its own step.
install: ## Install dependencies (frozen lockfile when CI is set)
	bun install $(if $(CI),--frozen-lockfile)
	@if [ -z "$(CI)" ] && [ -f lefthook.yml ] && command -v lefthook >/dev/null 2>&1; then lefthook install; fi
	@if [ -z "$(CI)" ] && [ -x node_modules/.bin/playwright ]; then node_modules/.bin/playwright install chromium; fi

# --ignore-lock keeps astro dev in the foreground when it detects an agent
# (otherwise it backgrounds itself and exits). Stop a stray one: bunx astro dev stop
dev: ## Astro dev server on PORT_WEB
	bunx astro dev --port $(PORT_WEB) --ignore-lock

lint: ## Biome for TS, JS, CSS and JSON; Prettier for .astro files
	bunx biome check .
	bunx prettier --check "**/*.astro"

format: ## Apply Biome and Prettier fixes
	bunx biome check --write .
	bunx prettier --write "**/*.astro"

check: ## Type-check pages, components and content (astro check)
	@bun --no-install -e 'require.resolve("@astrojs/check"); require.resolve("typescript")' >/dev/null 2>&1 || { echo 'Missing checker dependencies. Run bun add -d @astrojs/check typescript, then make check.' >&2; exit 1; }
	bunx --no-install astro check

test: ## Unit and component tests
	bunx vitest run

coverage: ## Unit and component tests with coverage
	bunx vitest run --coverage

build: ## Static build into dist/, then the acceptance tests over it
	bunx astro build
	bunx vitest run --config vitest.build.config.ts

preview: ## Serve the production build on PORT_WEB
	bunx astro preview --port $(PORT_WEB) --ignore-lock

e2e: ## Browser and accessibility tests against the production build
	node scripts/e2e.mjs

clean: ## Remove build output, test output and Astro caches
	rm -rf dist .astro node_modules/.astro coverage playwright-report test-results

# Records a real rr run for the Monitor stage. Needs every host up; see the script.
brand: ## Render the WebP icons, lockups, social cards and manifest from public/brand/*.svg
	bun scripts/brand.ts

CAPTURE_PROJECT ?= ../../opendata
CAPTURE_HOSTS ?= mbp,m4-mini,m1-mini
capture: ## Record the hero run: CAPTURE_PROJECT, CAPTURE_HOSTS
	scripts/capture-demo.sh "$(CAPTURE_PROJECT)" "$(CAPTURE_HOSTS)"

capture-commands: ## Record the Commands section against the rr repo's hosts
	scripts/capture-commands.sh

capture-agent: ## Record the Agent session's rr runs (a temp worktree of this repo)
	scripts/capture-agent.sh

deploy-check: build ## Serve dist/ through wrangler pages dev and check headers, canonical URLs and 404
	node --test tests/deploy.test.mjs

# Production normally deploys from Cloudflare's Git integration (AGENTS.md, Deploy).
# These upload from this machine and need CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID.
deploy-preview: build ## Upload dist/ as a preview for BRANCH (needs CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID)
	@test -n "$$CLOUDFLARE_API_TOKEN" -a -n "$$CLOUDFLARE_ACCOUNT_ID" || { echo "Set CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID." >&2; exit 1; }
	@test -n "$(BRANCH)" || { echo "Set BRANCH, e.g. make deploy-preview BRANCH=my-change." >&2; exit 1; }
	bunx wrangler pages deploy dist --branch "$(BRANCH)"

deploy: build ## Upload dist/ to production (needs CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID)
	@test -n "$$CLOUDFLARE_API_TOKEN" -a -n "$$CLOUDFLARE_ACCOUNT_ID" || { echo "Set CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID." >&2; exit 1; }
	bunx wrangler pages deploy dist --branch main
