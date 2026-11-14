import { satteri, satteriHeadingIdsPlugin } from "@astrojs/markdown-satteri";
import mdx from "@astrojs/mdx";
import react from "@astrojs/react";
import sitemap from "@astrojs/sitemap";
import tailwind from "@tailwindcss/vite";
import { defineConfig } from "astro/config";
import { site } from "./src/config.ts";
import { docsHastPlugins, shikiConfig } from "./src/lib/markdown.ts";

export default defineConfig({
  site: site.url,
  output: "static",
  // Directory output with trailing slashes: /docs/install/ is served from
  // docs/install/index.html, so canonicals, links and the sitemap agree as-is.
  trailingSlash: "always",
  build: { format: "directory" },
  integrations: [
    react(),
    mdx(),
    // Pages only: the error page and endpoints (.md, .txt, .png) stay out.
    sitemap({ filter: (page) => page.endsWith("/") && !page.includes("/404") }),
  ],
  markdown: {
    // Sätteri is Astro's default processor; heading ids come first so the
    // anchor plugin can read them.
    processor: satteri({ hastPlugins: [satteriHeadingIdsPlugin(), ...docsHastPlugins] }),
    shikiConfig,
  },
  vite: { plugins: [tailwind()] },
});
