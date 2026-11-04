import { satteri, satteriHeadingIdsPlugin } from "@astrojs/markdown-satteri";
import react from "@astrojs/react";
import sitemap from "@astrojs/sitemap";
import tailwind from "@tailwindcss/vite";
import { defineConfig } from "astro/config";
import { site } from "./src/config.ts";
import { docsHastPlugins, shikiTransformers } from "./src/lib/markdown.ts";

export default defineConfig({
  site: site.url,
  output: "static",
  // Directory output with trailing slashes: /docs/install/ is served from
  // docs/install/index.html, so canonicals, links and the sitemap agree as-is.
  trailingSlash: "always",
  build: { format: "directory" },
  integrations: [
    react(),
    // Pages only: the error page and endpoints (.md, .txt, .png) stay out.
    sitemap({ filter: (page) => page.endsWith("/") && !page.includes("/404") }),
  ],
  markdown: {
    // Sätteri is Astro's default processor; heading ids come first so the
    // anchor plugin can read them.
    processor: satteri({ hastPlugins: [satteriHeadingIdsPlugin(), ...docsHastPlugins] }),
    shikiConfig: {
      themes: { light: "github-light", dark: "github-dark-default" },
      // Both themes as CSS variables; global.css picks one per theme.
      defaultColor: false,
      transformers: shikiTransformers,
    },
  },
  vite: { plugins: [tailwind()] },
});
