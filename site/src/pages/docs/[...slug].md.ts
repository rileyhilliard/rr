import type { APIRoute, GetStaticPaths } from "astro";
import { getDocs } from "../../lib/collection";
import { type DocEntry, toMarkdownExport } from "../../lib/docs";

// Each docs page as Markdown at /docs/<id>.md, linked from the page head as rel="alternate".
export const getStaticPaths = (async () =>
  (await getDocs()).map((doc) => ({
    params: { slug: doc.id },
    props: doc,
  }))) satisfies GetStaticPaths;

export const GET: APIRoute<DocEntry> = ({ props }) =>
  new Response(toMarkdownExport(props), {
    headers: { "Content-Type": "text/markdown; charset=utf-8" },
  });
