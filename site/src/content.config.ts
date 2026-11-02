import { defineCollection } from "astro:content";
import { glob } from "astro/loaders";
import { docSchema } from "./content/schema";
import { docsDir, docsPattern } from "./lib/docs-source";

export const collections = {
  docs: defineCollection({
    loader: glob({ pattern: docsPattern, base: docsDir }),
    schema: docSchema,
  }),
};
