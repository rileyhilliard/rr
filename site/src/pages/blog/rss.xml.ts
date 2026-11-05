import rss from "@astrojs/rss";
import { site } from "../../config";
import { postHref } from "../../lib/blog";
import { getPosts } from "../../lib/collection";

export async function GET() {
  return rss({
    title: "rr blog: remote test runs and coding agents",
    description: "Practical guides to remote test runs and coding agents.",
    site: site.url,
    items: (await getPosts()).map((post) => ({
      title: post.data.title,
      description: post.data.description,
      pubDate: post.data.published,
      link: new URL(postHref(post.id), site.url).href,
    })),
  });
}
