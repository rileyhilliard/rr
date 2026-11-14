import { defineConfig, devices } from "@playwright/test";

const port = Number(process.env.PORT_E2E ?? 4322);
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: "./e2e",
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  use: { baseURL, trace: "retain-on-failure" },
  projects: [
    { name: "desktop", use: { browserName: "chromium", viewport: { width: 1280, height: 800 } } },
    { name: "mobile", use: { ...devices["Pixel 7"], browserName: "chromium" } },
  ],
  webServer: {
    // scripts/e2e.mjs builds first when dist/ is missing; this serves it the way a static host does.
    command: `bunx --no-install astro preview --host 127.0.0.1 --port ${port} --ignore-lock`,
    url: baseURL,
    reuseExistingServer: false,
  },
});
