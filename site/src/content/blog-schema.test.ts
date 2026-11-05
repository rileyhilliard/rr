import { describe, expect, it } from "vitest";
import { blogSchema } from "./blog-schema";

const valid = {
  title: "Remote tests",
  description: "x".repeat(120),
  published: "2026-11-01",
  keyword: "remote tests",
  feature: "parallel",
};

describe("blogSchema", () => {
  it("coerces dates and defaults optional fields", () => {
    expect(blogSchema.parse(valid)).toMatchObject({
      published: new Date("2026-11-01"),
      draft: false,
      relatedDocs: [],
    });
  });
  it.each([
    ["title", "", "1"],
    ["title", "x".repeat(49), "48"],
    ["headline", "x".repeat(111), "110"],
    ["headline", "", "1"],
    ["headline", "   ", "1"],
    ["description", "x".repeat(119), "120"],
    ["description", "x".repeat(161), "160"],
    ["keyword", "", "1"],
  ])("names the %s field and its limit", (field, value, limit) => {
    const result = blogSchema.safeParse({ ...valid, [field]: value });
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.issues[0]?.path).toEqual([field]);
      expect(result.error.issues[0]?.message).toContain(limit);
      expect(result.error.issues[0]?.message).toContain(field);
    }
  });
  it("accepts exact upper limits", () => {
    expect(
      blogSchema.safeParse({
        ...valid,
        title: "x".repeat(48),
        headline: "x".repeat(110),
        description: "x".repeat(160),
      }).success,
    ).toBe(true);
  });
  it("rejects missing or non-date values rather than coercing them to the Unix epoch", () => {
    for (const value of [null, 0, false])
      expect(blogSchema.safeParse({ ...valid, published: value }).success).toBe(false);
  });

  it("rejects invalid dates, unknown features and an update before publication", () => {
    for (const change of [
      { published: "yesterday" },
      { feature: "unknown" },
      { updated: "2026-10-31" },
    ])
      expect(blogSchema.safeParse({ ...valid, ...change }).success).toBe(false);
    expect(() => blogSchema.parse({ ...valid, updated: "2026-10-31" })).toThrow(
      "updated must be on or after published",
    );
    expect(blogSchema.safeParse({ ...valid, updated: valid.published }).success).toBe(true);
  });
});
