// @vitest-environment node
import { experimental_AstroContainer as AstroContainer } from "astro/container";
import { expect, it } from "vitest";
import CommandDemo from "./CommandDemo.astro";

it("names an unknown command and the allowed recordings at the component boundary", async () => {
  const container = await AstroContainer.create();
  await expect(
    container.renderToString(CommandDemo, { props: { caption: "A recording", command: "typo" } }),
  ).rejects.toThrow(
    "CommandDemo: unknown command 'typo' (allowed: init | doctor | status | run | test | logs)",
  );
});
