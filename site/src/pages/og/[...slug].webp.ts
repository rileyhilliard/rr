import type { APIRoute, GetStaticPaths } from "astro";
import satori from "satori";
import sharp from "sharp";
import { site } from "../../config";
import { getDocs } from "../../lib/collection";
import { type Card, ogCard, ogFonts, ogLogo } from "../../lib/og";

// One card per page: /og/index.webp for the landing page, /og/docs/<id>.webp per doc.
export const getStaticPaths = (async () => {
  const docs = await getDocs();
  const landing = { title: site.tagline, description: site.description, siteName: site.name };
  return [
    { params: { slug: "index" }, props: landing },
    ...docs.map((doc) => ({
      params: { slug: `docs/${doc.id}` },
      props: { title: doc.data.title, description: doc.data.description, siteName: site.name },
    })),
  ];
}) satisfies GetStaticPaths;

export const GET: APIRoute<Card> = async ({ props }) => {
  const svg = await satori(ogCard({ ...props, logo: await ogLogo() }) as never, {
    width: 1200,
    height: 630,
    fonts: await ogFonts(),
  });
  const webp = await sharp(Buffer.from(svg)).webp({ quality: 90 }).toBuffer();
  return new Response(new Uint8Array(webp), { headers: { "Content-Type": "image/webp" } });
};
