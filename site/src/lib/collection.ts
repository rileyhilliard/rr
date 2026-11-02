import { getCollection } from "astro:content";
import type { DocEntry } from "./docs";

/** The only module pages import astro:content through. */
export async function getDocs(): Promise<DocEntry[]> {
  const entries = await getCollection("docs");
  return entries.map(({ id, data, body }) => ({ id, data, body: body ?? "" }));
}
