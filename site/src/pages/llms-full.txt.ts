import type { APIRoute } from "astro";
import { docsGroups, site } from "../config";
import { getDocs } from "../lib/collection";
import { docsNav, llmsFull } from "../lib/docs";

export const GET: APIRoute = async () => {
  const docs = await getDocs();
  return new Response(llmsFull(site, docsNav(docs, docsGroups), docs), {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
