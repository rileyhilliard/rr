// Renders docs Markdown through the same Sätteri processor and plugins as astro.config.mjs,
// so tests read the markup the site ships.
import { createSatteriMarkdownProcessor, satteriHeadingIdsPlugin } from "@astrojs/markdown-satteri";
import { docsHastPlugins, shikiConfig } from "../../src/lib/markdown";

// Shiki's inlined wasm reads Response.bytes, which happy-dom's Response lacks (AGENTS.md,
// Build workarounds). Node's has it, so this only fills the gap under happy-dom.
if (typeof Response !== "undefined" && !("bytes" in Response.prototype)) {
  Object.defineProperty(Response.prototype, "bytes", {
    configurable: true,
    async value(this: Response) {
      return new Uint8Array(await this.arrayBuffer());
    },
  });
}

export async function renderDocsMarkdown(markdown: string) {
  const processor = await createSatteriMarkdownProcessor({
    hastPlugins: [satteriHeadingIdsPlugin(), ...docsHastPlugins],
    syntaxHighlight: "shiki",
    shikiConfig,
  });
  return processor.render(markdown);
}
