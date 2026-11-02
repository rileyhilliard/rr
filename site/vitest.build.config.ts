import { defineConfig } from "vitest/config";

// The acceptance tests read dist/ after `astro build`; `make build` runs them.
export default defineConfig({
  test: {
    environment: "node",
    include: ["tests/build.test.ts"],
    testTimeout: 180_000,
  },
});
