import { existsSync, readdirSync, readFileSync } from "node:fs";
import AxeBuilder from "@axe-core/playwright";
import { expect, type Page, test } from "@playwright/test";
import { site } from "../src/config";
import { isDraft } from "../src/lib/blog";
import { docId, docsDir, isSitePage } from "../src/lib/docs-source";

// The first docs page in the docs source stands in for every docs page.
const firstDoc = readdirSync(docsDir).filter(isSitePage).sort()[0] ?? "";
const firstPost = (existsSync("src/content/blog") ? readdirSync("src/content/blog") : [])
  .filter(
    (file) => file.endsWith(".mdx") && !isDraft(readFileSync(`src/content/blog/${file}`, "utf8")),
  )
  .sort()[0];
const BLOG_POST = firstPost ? `/blog/${firstPost.slice(0, -4)}/` : undefined;
const DOC = `/docs/${docId(firstDoc)}/`;
const [firstInstall, secondInstall] = site.install;

function collectErrors(page: Page) {
  const errors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));
  return errors;
}

/**
 * Scrolls the page through, as a visitor would, so every scroll reveal fires, then
 * waits for CSS animations and transitions to finish. axe then checks the page as
 * people read it, not mid-fade.
 */
async function settle(page: Page) {
  await page.evaluate(async () => {
    for (let y = 0; y < document.body.scrollHeight; y += innerHeight / 2) {
      scrollTo(0, y);
      await new Promise((resolve) => requestAnimationFrame(resolve));
    }
    scrollTo(0, 0);
    await Promise.all(
      document
        .getAnimations()
        .filter(
          (animation) =>
            animation.effect?.getComputedTiming().iterations !== Number.POSITIVE_INFINITY,
        )
        .map((animation) => animation.finished.catch(() => undefined)),
    );
  });
}

// The site is dark only, like the rr TUI; axe runs against that theme, with and
// without a reduced-motion preference (the Monitor stage renders differently).
for (const path of ["/", DOC, "/blog/", ...(BLOG_POST ? [BLOG_POST] : [])]) {
  for (const reducedMotion of ["no-preference", "reduce"] as const) {
    test(`${path} with motion ${reducedMotion} passes axe with no console errors`, async ({
      page,
    }) => {
      const errors = collectErrors(page);
      await page.emulateMedia({ colorScheme: "dark", reducedMotion });
      await page.goto(path);
      await settle(page);
      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();
      expect(results.violations).toEqual([]);
      expect(errors).toEqual([]);
    });
  }
}

test("install tabs switch commands once hydrated", async ({ page }) => {
  test.skip(!secondInstall, "only one install option");
  await page.goto("/#install");
  await page.getByRole("tab", { name: secondInstall?.label }).click();
  await expect(page.locator("#install").getByRole("tabpanel")).toContainText(
    secondInstall?.command ?? "",
  );
});

test("the Monitor stage replays the run and its transcript switches views", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.goto("/");
  const pause = page.getByRole("button", { name: "Pause" }).first();
  await expect(pause).toBeVisible();
  await page.getByRole("button", { name: "Agent", exact: true }).click();
  await expect(page.getByRole("region", { name: "JSON events" })).toBeVisible();
  await pause.click();
  await expect(page.getByRole("button", { name: "Play" }).first()).toBeVisible();
});

for (const path of ["/", "/blog/", ...(BLOG_POST ? [BLOG_POST] : [])]) {
  for (const width of [1440, 375]) {
    test(`${path} never scrolls sideways at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(path);
      const { scroll, client } = await page.evaluate(() => ({
        scroll: document.documentElement.scrollWidth,
        client: document.documentElement.clientWidth,
      }));
      expect(scroll).toBeLessThanOrEqual(client);
    });
  }
}

if (BLOG_POST)
  for (const width of [1440, 375]) {
    test(`blog TOC targets clear the sticky header at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ reducedMotion: "reduce" });
      await page.goto(BLOG_POST);
      const heading = page.locator(".prose h2[id], .prose h3[id]").first();
      test.skip((await heading.count()) === 0, "post has no TOC headings");
      const id = await heading.getAttribute("id");
      if (width < 1280) await page.getByText("On this page", { exact: true }).first().click();
      const toc =
        width >= 1280
          ? page.locator("[data-toc]")
          : page.locator('details nav[aria-label="On this page"]');
      await toc.locator(`a[href="#${id}"]`).click();
      const headerBottom = await page
        .locator("body > header")
        .evaluate((el) => el.getBoundingClientRect().bottom);
      await expect
        .poll(async () => heading.evaluate((el) => el.getBoundingClientRect().top))
        .toBeGreaterThanOrEqual(headerBottom);
    });
  }

test("the queue hands a freed host to the waiting run", async ({ page }) => {
  await page.goto("/#queue");
  const queue = page.locator("#queue");
  await expect(queue).toContainText("waiting up to");
  await queue.getByRole("button", { name: /Finish run on m1-linux/ }).click();
  await expect(queue).toContainText("Nobody waiting");
});

test("command tabs switch by keyboard and update the URL", async ({ page }) => {
  await page.goto("/#commands");
  const tabs = page.locator("#commands").getByRole("tab");
  await tabs.first().click();
  await page.keyboard.press("ArrowDown");
  await expect(tabs.nth(1)).toHaveAttribute("aria-selected", "true");
  await expect(page).toHaveURL(/#cmd-/);
});

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false });

  test("docs menu, install command, monitor frame and a deep-linked command all show", async ({
    page,
    isMobile,
  }) => {
    await page.goto(DOC);
    if (isMobile) {
      await page.getByText("Docs menu").click();
      await expect(page.getByRole("navigation", { name: "Docs menu" })).toBeVisible();
    } else {
      await expect(page.getByRole("navigation", { name: "Docs" })).toBeVisible();
    }
    await page.goto("/");
    await expect(page.locator("#install").getByRole("tabpanel")).toContainText(
      firstInstall?.command ?? "",
    );
    await expect(page.locator('article[data-status="running"]').first()).toBeAttached();
    await page.goto("/#cmd-doctor");
    await expect(page.locator("#cmd-doctor")).toBeVisible();
  });
});
