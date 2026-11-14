// Runs the Playwright tests against the production build. Exits 127 when
// Playwright or its Chromium is missing, so `stacks verify` records the browser
// step as blocked by the environment rather than as a failing test. `make e2e`
// calls this too, but make turns every failure into status 2, so verify runs it
// directly.
import { spawnSync } from "node:child_process";
import { accessSync, existsSync } from "node:fs";
import { createRequire } from "node:module";

function chromiumInstalled() {
  try {
    const { chromium } = createRequire(import.meta.url)("@playwright/test");
    accessSync(chromium.executablePath());
    return true;
  } catch {
    return false;
  }
}

if (!chromiumInstalled()) {
  console.error(
    "Playwright's Chromium is missing. Run bunx playwright install chromium, then make e2e.",
  );
  process.exit(127);
}

const run = (command, args) => {
  const result = spawnSync(command, args, { stdio: "inherit" });
  if (result.status !== 0) process.exit(result.status ?? 1);
};

if (!existsSync("dist/index.html")) run("bunx", ["--no-install", "astro", "build"]);
run("bunx", ["--no-install", "playwright", "test"]);
