import { track } from "../lib/analytics";
import { type EventName, Events } from "../lib/events";

// The one module script every page loads. It isn't an island: docs pages ship no
// hydrated component, so copy buttons, section tracking and the TOC highlight
// are plain DOM code. Each part does nothing on a page without its markup.

async function copyWithFeedback(button: HTMLButtonElement, read: () => Promise<string> | string) {
  const label = button.textContent;
  try {
    await navigator.clipboard.writeText(await read());
    button.textContent = "Copied";
  } catch {
    button.textContent = "Copy failed";
  }
  setTimeout(() => {
    button.textContent = label;
  }, 1500);
}

function setupCodeCopy() {
  for (const frame of document.querySelectorAll<HTMLElement>(".code-frame")) {
    const code = frame.querySelector("code");
    if (!code) continue;
    const button = document.createElement("button");
    button.type = "button";
    button.className = "code-copy";
    button.textContent = "Copy";
    button.addEventListener("click", () => copyWithFeedback(button, () => code.textContent ?? ""));
    frame.append(button);
  }
}

function setupCopyPage() {
  const button = document.querySelector<HTMLButtonElement>("[data-copy-page]");
  const href = button?.dataset.copyPage;
  if (!button || !href) return;
  button.hidden = false;
  button.addEventListener("click", () =>
    copyWithFeedback(button, async () => (await fetch(href)).text()),
  );
}

function setupTocHighlight() {
  const links = [...document.querySelectorAll<HTMLAnchorElement>("[data-toc] a[href^='#']")];
  const byId = new Map(links.map((link) => [decodeURIComponent(link.hash.slice(1)), link]));
  const headings = [...byId.keys()]
    .map((id) => document.getElementById(id))
    .filter((heading) => heading !== null);
  if (headings.length === 0) return;
  const observer = new IntersectionObserver(
    (entries) => {
      const visible = entries.find((entry) => entry.isIntersecting);
      if (!visible) return;
      for (const link of links) link.removeAttribute("aria-current");
      byId.get(visible.target.id)?.setAttribute("aria-current", "location");
    },
    { rootMargin: "0px 0px -70% 0px" },
  );
  for (const heading of headings) observer.observe(heading);
}

/**
 * Copy buttons beside command lines (CopyLine.astro): hidden until JavaScript runs.
 * data-track names the event from the catalog; data-track-method is its method.
 */
function setupCopyLines() {
  const tracked = new Set<string>(Object.values(Events));
  for (const button of document.querySelectorAll<HTMLButtonElement>("button[data-copy]")) {
    button.hidden = false;
    button.addEventListener("click", async () => {
      await copyWithFeedback(button, () => button.dataset.copy ?? "");
      const event = button.dataset.track;
      if (event && tracked.has(event)) {
        const method = button.dataset.trackMethod;
        track(event as EventName, method ? { method } : undefined);
      }
    });
  }
}

/** Sends section_viewed once per [data-track-view] section that scrolls into view. */
function setupSectionViews() {
  const sections = document.querySelectorAll<HTMLElement>("[data-track-view]");
  if (sections.length === 0 || typeof IntersectionObserver === "undefined") return;
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        track(Events.sectionViewed, { id: entry.target.id });
        observer.unobserve(entry.target);
      }
    },
    { threshold: 0.3 },
  );
  for (const section of sections) observer.observe(section);
}

/** CommandTabs announces tab switches as a DOM event; the island stays free of analytics. */
function setupCommandTabTracking() {
  window.addEventListener("rr:command-tab", (event) => {
    const name = (event as CustomEvent<{ name?: string }>).detail?.name;
    if (name) track(Events.commandTabOpened, { name });
  });
}

/**
 * Reveals each [data-reveal] section once, as its top passes three quarters of the
 * way down the viewport, and marks it settled when its entrance transitions end so
 * hover transitions lose the reveal delays. The CSS only hides content under html.js
 * with motion allowed, so without this script nothing is hidden; a section that
 * takes focus is revealed at once.
 */
function setupReveals() {
  const blocks = [...document.querySelectorAll<HTMLElement>("[data-reveal]")];
  if (blocks.length === 0) return;
  const show = (el: Element) => {
    if (el.classList.contains("is-visible")) return;
    el.classList.add("is-visible");
    // getAnimations flushes styles, so the entrance transitions exist by now.
    const entrances = el
      .getAnimations({ subtree: true })
      .filter((animation) => animation instanceof CSSTransition);
    void Promise.allSettled(entrances.map((animation) => animation.finished)).then(() =>
      el.classList.add("is-settled"),
    );
  };
  if (typeof IntersectionObserver === "undefined") {
    blocks.forEach(show);
    return;
  }
  document.documentElement.classList.add("js");
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        show(entry.target);
        observer.unobserve(entry.target);
      }
    },
    { rootMargin: "0px 0px -25% 0px" },
  );
  for (const block of blocks) {
    observer.observe(block);
    block.addEventListener("focusin", () => show(block), { once: true });
  }
}

setupReveals();
setupCodeCopy();
setupCopyLines();
setupSectionViews();
setupCommandTabTracking();
setupCopyPage();
setupTocHighlight();
