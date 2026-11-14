import { z } from "astro/zod";

const date = (field: string) =>
  z.preprocess(
    (value) => (typeof value === "string" ? new Date(value) : value),
    z.date({ message: `${field} must be a valid date` }),
  );

export const blogSchema = z
  .object({
    title: z
      .string()
      .trim()
      .min(1, "title must have at least 1 character")
      .max(48, "title must have at most 48 characters"),
    headline: z
      .string()
      .trim()
      .min(1, "headline must have at least 1 character")
      .max(110, "headline must have at most 110 characters")
      .optional(),
    description: z
      .string()
      .trim()
      .min(120, "description must have at least 120 characters")
      .max(160, "description must have at most 160 characters"),
    published: date("published"),
    updated: date("updated").optional(),
    keyword: z.string().trim().min(1, "keyword must have at least 1 character"),
    feature: z.enum([
      "agents",
      "worktrees",
      "remote-exec",
      "hosts",
      "parallel",
      "setup",
      "output",
      "locking",
      "sync",
    ]),
    relatedDocs: z.array(z.string()).default([]),
    draft: z.boolean().default(false),
  })
  .refine((post) => !post.updated || post.updated >= post.published, {
    path: ["updated"],
    message: "updated must be on or after published",
  });

export type BlogData = z.infer<typeof blogSchema>;
