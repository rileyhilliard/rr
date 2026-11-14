import { getCollection } from "astro:content";
import { publishedPosts, relatedDocs } from "./blog";
import type { DocEntry } from "./docs";

/** The only module pages import astro:content through. */
export async function getDocs(): Promise<DocEntry[]> {
  const entries = await getCollection("docs");
  return entries.map(({ id, data, body }) => ({ id, data, body: body ?? "" }));
}

/** All blog consumers share draft filtering, ordering and docs validation. */
export async function getPosts() {
  const [posts, docs] = await Promise.all([getCollection("blog"), getDocs()]);
  for (const post of posts) relatedDocs(post, docs);
  return publishedPosts(posts);
}
