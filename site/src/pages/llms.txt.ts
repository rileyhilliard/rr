import type { APIRoute } from "astro";
import { docsGroups, site } from "../config";
import { llmsBlog } from "../lib/blog";
import { getDocs, getPosts } from "../lib/collection";
import { docsNav, llmsIndex } from "../lib/docs";

export const GET: APIRoute = async () =>
  new Response(
    llmsIndex(site, docsNav(await getDocs(), docsGroups)) + llmsBlog(await getPosts(), site.url),
    {
      headers: { "Content-Type": "text/plain; charset=utf-8" },
    },
  );
