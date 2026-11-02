/// <reference types="vitest/config" />
import { getViteConfig } from "astro/config";

export default getViteConfig({
  test: {
    environment: "happy-dom",
    globals: true,
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./tests/setup.ts"],
    env: { PUBLIC_POSTHOG_KEY: "" },
    coverage: { provider: "v8", include: ["src/**/*.{ts,tsx}"] },
  },
});
