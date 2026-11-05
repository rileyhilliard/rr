// Analytics in a real browser, ported from stacks-api's tests/browser/analytics.spec.ts:
// the site built with a test key and pointed at a PostHog host that exists only here.
// Every request to it is answered below, so nothing leaves the test.
import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync } from "node:fs";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { basename, extname, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";
import { gunzipSync } from "node:zlib";
import { test as base, expect, type Page, type Route } from "@playwright/test";
import { docId, docsDir, isSitePage } from "../src/lib/docs-source";
import { everythingOn } from "../tests/helpers/posthog-remote-config";

const key = "phc_browser_test";
const host = "https://posthog.test";
// Text a visitor types; it must not reach a recording or an autocapture event.
const typed = "zebrafish observatory";
const siteDir = fileURLToPath(new URL("../", import.meta.url));
const posthogDist = join(siteDir, "node_modules/posthog-js/dist/");
const firstDoc = readdirSync(docsDir).filter(isSitePage).sort()[0] ?? "";
const DOC = `/docs/${docId(firstDoc)}/`;

// The project's remote config with everything on, sent uncompressed so the test can
// read what posthog-js sends.
const remoteConfig = { ...everythingOn, supportedCompression: [] };

type Sent = { event: string; properties: Record<string, unknown>; at: number };

const person =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36";

const types: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript",
  ".css": "text/css",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".webp": "image/webp",
  ".woff2": "font/woff2",
  ".json": "application/json",
  ".txt": "text/plain",
  ".xml": "application/xml",
};

// A static host for the build, serving directories by their index.html, as Pages does.
async function serve(root: string) {
  const server = createServer((request, response) => {
    const path = normalize(decodeURIComponent(new URL(request.url ?? "/", "http://x").pathname));
    let file = join(root, path);
    if (existsSync(file) && statSync(file).isDirectory()) file = join(file, "index.html");
    if (!file.startsWith(root) || !existsSync(file)) {
      response.writeHead(404, { "content-type": "text/html" });
      response.end(readFileSync(join(root, "404.html")));
      return;
    }
    response.writeHead(200, { "content-type": types[extname(file)] ?? "application/octet-stream" });
    response.end(readFileSync(file));
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;
  return { port, close: () => new Promise<void>((resolve) => server.close(() => resolve())) };
}

const test = base.extend<object, { siteURL: string }>({
  siteURL: [
    async ({ browserName }, use) => {
      expect(browserName).toBe("chromium");
      const out = mkdtempSync(join(tmpdir(), "rr-analytics-browser-"));
      let server: Awaited<ReturnType<typeof serve>> | undefined;
      try {
        const build = spawnSync("bunx", ["--no-install", "astro", "build", "--outDir", out], {
          cwd: siteDir,
          encoding: "utf8",
          env: { ...process.env, PUBLIC_POSTHOG_KEY: key, PUBLIC_POSTHOG_HOST: host },
        });
        expect(build.status, build.stderr).toBe(0);
        server = await serve(out);
        // A name other than localhost: posthog-js never records network requests there.
        await use(`http://rr.test:${server.port}`);
      } finally {
        await server?.close();
        rmSync(out, { recursive: true, force: true });
      }
    },
    { scope: "worker", timeout: 120000 },
  ],
});

// posthog-js posts JSON, gzipped until remote config says otherwise, or (by beacon on
// unload) a form field `data` holding base64 JSON. The JSON is a batch
// (`{ api_key, batch }`), a list or a single event.
function eventsIn(body: Buffer): Omit<Sent, "at">[] {
  const gunzipped = (bytes: Buffer) =>
    bytes[0] === 0x1f && bytes[1] === 0x8b ? gunzipSync(bytes) : bytes;
  let text = gunzipped(body).toString("utf8");
  if (text.startsWith("data=")) {
    const data = new URLSearchParams(text).get("data") ?? "";
    text = gunzipped(Buffer.from(data, "base64")).toString("utf8");
  }
  const payload: unknown = JSON.parse(text);
  const batch =
    typeof payload === "object" && payload !== null && "batch" in payload ? payload.batch : payload;
  const list = Array.isArray(batch) ? batch : [batch];
  return list.filter(
    (item): item is Omit<Sent, "at"> =>
      typeof item === "object" && item !== null && "event" in item && "properties" in item,
  );
}

async function servePostHog(route: Route, sent: Sent[]) {
  const url = new URL(route.request().url());
  if (url.pathname === `/array/${key}/config.js`) {
    // What PostHog's CDN serves: the config, assigned for posthog-js to pick up.
    return route.fulfill({
      contentType: "application/javascript",
      body: `window._POSTHOG_REMOTE_CONFIG = window._POSTHOG_REMOTE_CONFIG || {};
window._POSTHOG_REMOTE_CONFIG[${JSON.stringify(key)}] = { config: ${JSON.stringify(remoteConfig)}, siteApps: [] };`,
    });
  }
  if (url.pathname === `/array/${key}/config`) return route.fulfill({ json: remoteConfig });
  if (url.pathname.startsWith("/static/")) {
    // posthog-js's own scripts (the recorder, web vitals, heatmaps' dead clicks) from
    // the installed package, as PostHog's CDN would serve them for this version.
    const file = join(posthogDist, basename(url.pathname));
    if (!existsSync(file)) return route.fulfill({ status: 404, body: "" });
    return route.fulfill({ contentType: "application/javascript", body: readFileSync(file) });
  }
  const body = route.request().postDataBuffer();
  for (const event of body?.length ? eventsIn(body) : []) sent.push({ ...event, at: Date.now() });
  return route.fulfill({ json: { status: 1 } });
}

async function open(page: Page, siteURL: string) {
  const sent: Sent[] = [];
  // posthog-js drops every event from a browser that looks automated (navigator.webdriver,
  // a HeadlessChrome brand), which is right for bots; this browser stands in for a person.
  // On the context, so a tab a click opens isn't dropped as a bot either.
  await page.context().addInitScript(() => {
    Object.defineProperty(navigator, "webdriver", { get: () => false });
    Object.defineProperty(navigator, "userAgentData", { get: () => undefined });
  });
  // On the context, so a page's last batch, sent as it unloads, is caught too.
  await page.context().route(`${host}/**`, (route) => servePostHog(route, sent));
  await page.goto(`${siteURL}/`);
  return sent;
}

// The mobile project emulates a phone; one Chromium run covers what this checks.
test.skip(({ isMobile }) => isMobile, "desktop Chromium only");

test.use({
  userAgent: person,
  launchOptions: {
    args: ["--host-resolver-rules=MAP rr.test 127.0.0.1"],
  },
});

// posthog-js sends in batches every few seconds, and buffers web vitals for longer.
const delivery = { timeout: 15000 };

const banner = (page: Page) => page.getByRole("region", { name: "Analytics consent" });
const named = (sent: Sent[], event: string, since = 0) =>
  sent.filter((item) => item.event === event && item.at >= since);

// rr has no text field of its own; this stands in for one, so the test can check what
// happens to typed text. It sits in main, outside any a, button, label or form.
async function typeIntoAField(page: Page) {
  await page.evaluate(() => {
    const field = document.createElement("input");
    field.setAttribute("aria-label", "Notes");
    document.querySelector("main")?.append(field);
  });
  const field = page.getByRole("textbox", { name: "Notes" });
  await field.fill(typed);
  await field.press("Tab");
}

// Picks an install method, copies a command and reads a docs page: the clicks and
// changes a visitor makes.
async function useSite(page: Page, siteURL: string) {
  await page.goto(`${siteURL}/`);
  const install = page.locator("#install");
  await install.getByRole("tab").nth(1).click();
  await install.getByRole("button", { name: /copy/i }).first().click();
  await typeIntoAField(page);
  await page.goto(`${siteURL}${DOC}`);
  // Leaving the page flushes what posthog-js buffers: heatmaps, recordings, web vitals.
  await page.goto(`${siteURL}/`);
}

test("counts a cookieless visitor without recordings or heatmaps, and sends web vitals", async ({
  page,
  siteURL,
}) => {
  const sent = await open(page, siteURL);
  await expect(banner(page)).toBeVisible();
  await useSite(page, siteURL);
  await expect.poll(() => named(sent, "$pageview").length, delivery).toBeGreaterThan(1);
  await expect.poll(() => named(sent, "$web_vitals").length, delivery).toBeGreaterThan(0);
  for (const vitals of named(sent, "$web_vitals")) {
    expect(vitals.properties).toMatchObject({ site: "rr", $cookieless_mode: true });
  }
  expect(named(sent, "$snapshot")).toEqual([]);
  expect(named(sent, "$$heatmap")).toEqual([]);
});

test("records nothing and keeps no heatmaps after Decline", async ({ page, siteURL }) => {
  const sent = await open(page, siteURL);
  await banner(page).getByRole("button", { name: "Decline" }).click();
  await useSite(page, siteURL);
  await expect.poll(() => named(sent, "$pageview").length, delivery).toBeGreaterThan(1);
  expect(named(sent, "$snapshot")).toEqual([]);
  expect(named(sent, "$$heatmap")).toEqual([]);
});

test("records after Accept, with typed text masked and no network payloads", async ({
  page,
  siteURL,
}) => {
  const sent = await open(page, siteURL);
  await banner(page).getByRole("button", { name: "Accept" }).click();
  await page.goto(`${siteURL}/`);
  // A request the page makes while recording, with a header and a body no recording may hold.
  const probe = { header: "probe-header-7c41", body: "probe-body-7c41" };
  await page.evaluate(
    ({ header, body }) =>
      fetch("/probe", { method: "POST", headers: { "x-probe": header }, body }).catch(() => null),
    probe,
  );
  await typeIntoAField(page);
  await page.locator("#install").getByRole("tab").nth(1).click();
  // Staying on the page: recordings and heatmaps are sent on their own timers. Wait for
  // the recording to hold the typed field's value as rrweb masks it (one * per
  // character), so the checks below read recording data that carries the input.
  const masked = "*".repeat(typed.length);
  const recordings = () => JSON.stringify(named(sent, "$snapshot"));
  await expect.poll(() => recordings().includes(masked), delivery).toBe(true);
  await expect.poll(() => named(sent, "$$heatmap").length, delivery).toBeGreaterThan(0);
  // Whole-payload scans: the typed text, the probe's header and body must appear nowhere.
  expect(recordings()).not.toContain(typed.split(" ")[0]);
  expect(recordings()).not.toContain(probe.header);
  expect(recordings()).not.toContain(probe.body);
  // No network plugin at all: neither payloads, headers nor timing are recorded.
  expect(recordings()).not.toContain("rrweb/network@1");
  expect(recordings()).not.toContain("requestHeaders");
});

test("autocapture counts clicks and sends no typed text", async ({ page, siteURL }) => {
  const sent = await open(page, siteURL);
  await typeIntoAField(page);
  const install = page.locator("#install");
  const tab = install.getByRole("tab").nth(1);
  const tabLabel = (await tab.textContent())?.trim();
  await tab.click();
  await install.getByRole("button", { name: /copy/i }).first().click();
  // A link opened in a new tab, so this page and its batches stay.
  await page
    .getByRole("contentinfo")
    .getByRole("link", { name: "Documentation", exact: true })
    .click({ modifiers: ["ControlOrMeta"] });
  await expect
    .poll(() => named(sent, "$autocapture").map((click) => click.properties.$el_text), delivery)
    .toEqual(expect.arrayContaining([tabLabel, "Documentation"]));
  for (const click of named(sent, "$autocapture")) {
    expect(click.properties).toMatchObject({
      site: "rr",
      $cookieless_mode: true,
      $current_url: `${siteURL}/`,
    });
  }
  const link = named(sent, "$autocapture").find(
    (click) => click.properties.$el_text === "Documentation",
  );
  expect(link?.properties.$elements_chain).toContain('attr__href="/docs/"');
  // Every event, not only clicks: typed text could ride on any property of any event.
  expect(JSON.stringify(sent)).not.toContain(typed.split(" ")[0]);
});

test("stops after a decline on another steamfield.io site, on the next page load", async ({
  page,
  context,
  siteURL,
}) => {
  const sent = await open(page, siteURL);
  await banner(page).getByRole("button", { name: "Accept" }).click();
  await page.goto(`${siteURL}/`);
  await page.locator("#install").getByRole("tab").nth(1).click();
  // Recording, before the decline.
  await expect.poll(() => named(sent, "$snapshot").length, delivery).toBeGreaterThan(0);
  // The sibling's Decline: sf-consent becomes denied and PostHog's consent cookie goes.
  const { hostname } = new URL(siteURL);
  await context.addCookies([{ name: "sf-consent", value: "denied", domain: hostname, path: "/" }]);
  const cookies = await context.cookies();
  await context.clearCookies({ name: `__ph_opt_in_out_${key}` });
  expect(cookies.some((cookie) => cookie.name === `__ph_opt_in_out_${key}`)).toBe(true);
  await page.goto(`${siteURL}${DOC}`);
  await page.goBack();
  // A fresh load: Playwright's Chromium runs with the back-forward cache off, so the
  // restored-page path is covered by analytics.test.ts's pageshow tests instead.
  const since = Date.now();
  await page.locator("#install").getByRole("tab").first().click();
  await typeIntoAField(page);
  await page.goto(`${siteURL}${DOC}`);
  await expect.poll(() => named(sent, "$pageview", since).length, delivery).toBeGreaterThan(0);
  expect(named(sent, "$snapshot", since)).toEqual([]);
  expect(named(sent, "$$heatmap", since)).toEqual([]);
});
