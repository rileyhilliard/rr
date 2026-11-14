import { defineCollection } from "astro:content";
import { glob } from "astro/loaders";
import { blogSchema } from "./content/blog-schema";
import { docSchema } from "./content/schema";
import { docsDir, docsPattern } from "./lib/docs-source";

export const collections = {
  blog: defineCollection({
    loader: glob({
      pattern: "*.mdx",
      base: "./src/content/blog",
      // Article URLs follow the filename, even if frontmatter includes a slug.
      generateId: ({ entry }) => entry.replace(/\.mdx$/, ""),
    }),
    schema: blogSchema,
  }),
  docs: defineCollection({
    loader: glob({ pattern: docsPattern, base: docsDir }),
    schema: docSchema,
  }),
};
