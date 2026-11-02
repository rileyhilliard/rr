import { z } from "astro/zod";
import { docsGroups } from "../config";

const groupIds = docsGroups.map((group) => group.id) as [string, ...string[]];

/** Frontmatter for every page under src/content/docs/. */
export const docSchema = z.object({
  title: z.string().trim().min(1),
  /** Meta description, docs-home card text and the llms.txt summary. */
  description: z.string().trim().min(1).max(160),
  group: z.enum(groupIds),
  /** Position inside the group; ties sort by title. */
  order: z.number().int().default(100),
  /** Shorter label for the sidebar and pager. */
  sidebarLabel: z.string().trim().min(1).optional(),
});

export type DocData = z.infer<typeof docSchema>;
