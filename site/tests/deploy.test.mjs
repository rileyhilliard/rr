// Serves the built dist/ through `wrangler pages dev`, Cloudflare's local Pages
// runtime, reading wrangler.jsonc the way a deploy does. Needs no account.
// Run after `astro build` (make deploy-check).
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtempSync, readdirSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, test } from "node:test";

let base = "";
let child;
let state = "";
let output = "";

async function freePort() {
  const socket = createServer().listen(0, "127.0.0.1");
  await once(socket, "listening");
  const { port } = socket.address();
  await new Promise((resolve) => socket.close(resolve));
  return port;
}

before(async () => {
  const port = await freePort();
  state = mkdtempSync(join(tmpdir(), "pages-dev-"));
  child = spawn(
    process.execPath,
    [
      "node_modules/wrangler/bin/wrangler.js",
      "pages",
      "dev",
      "--ip",
      "127.0.0.1",
      "--port",
      String(port),
      "--inspector-port",
      "0",
      "--persist-to",
      state,
    ],
    {
      detached: true,
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, WRANGLER_SEND_METRICS: "false", BROWSER: "none" },
    },
  );
  const keep = (data) => {
    output = (output + data.toString()).slice(-16_000);
  };
  child.stdout.on("data", keep);
  child.stderr.on("data", keep);
  base = `http://127.0.0.1:${port}`;
  const deadline = Date.now() + 60_000;
  for (;;) {
    if (child.exitCode !== null) {
      assert.fail(`wrangler pages dev exited:\n${output}`);
    }
    try {
      if ((await fetch(base, { signal: AbortSignal.timeout(1_000) })).ok) break;
    } catch {}
    if (Date.now() > deadline) {
      assert.fail(`wrangler pages dev never answered:\n${output}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
});

after(async () => {
  if (child?.exitCode === null && child.pid !== undefined) {
    const closed = once(child, "close");
    process.kill(-child.pid, "SIGTERM");
    const force = setTimeout(() => process.kill(-child.pid, "SIGKILL"), 5_000);
    await closed;
    clearTimeout(force);
  }
  if (state) rmSync(state, { recursive: true, force: true });
});

const get = (path) => fetch(new URL(path, base), { redirect: "manual" });

test("hashed assets are cached for a year as immutable", async () => {
  const asset = readdirSync("dist/_astro").find((name) => name.endsWith(".css"));
  assert.ok(asset, "astro build emitted no CSS under dist/_astro");
  const response = await get(`/_astro/${asset}`);
  assert.equal(response.status, 200);
  assert.equal(response.headers.get("cache-control"), "public, max-age=31536000, immutable");
});

test("pages carry the security headers and a revalidating cache", async () => {
  const response = await get("/docs/");
  assert.equal(response.status, 200);
  assert.equal(response.headers.get("x-content-type-options"), "nosniff");
  assert.equal(response.headers.get("referrer-policy"), "strict-origin-when-cross-origin");
  assert.equal(response.headers.get("x-frame-options"), "DENY");
  assert.match(response.headers.get("cache-control") ?? "", /max-age=0/);
});

test("Pages gives each docs page one trailing-slash URL without redirect rules", async () => {
  for (const path of ["/docs", "/docs/index.html"]) {
    const response = await get(path);
    assert.equal(response.status, 308, path);
    assert.equal(response.headers.get("location"), "/docs/", path);
  }
});

test("Markdown exports and llms.txt are served as files", async () => {
  assert.equal((await get("/llms.txt")).status, 200);
  const exported = readdirSync("dist/docs", { recursive: true, encoding: "utf8" }).find((file) =>
    file.endsWith(".md"),
  );
  assert.ok(exported, "astro build emitted no Markdown export under dist/docs");
  assert.equal((await get(`/docs/${exported}`)).status, 200);
});

test("unknown paths get the built 404 page", async () => {
  const response = await get("/no-such-page/");
  assert.equal(response.status, 404);
  assert.match(await response.text(), /Page not found/);
});

test("sitemap.xml redirects to the sitemap index", async () => {
  const response = await get("/sitemap.xml");
  assert.equal(response.status, 301);
  assert.equal(response.headers.get("location"), "/sitemap-index.xml");
});
