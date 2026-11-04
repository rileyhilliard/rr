import { copyText, enhanceCopyButtons } from "@steamfield/ui/copy";
import { spyToc } from "@steamfield/ui/docs";
import { initReveal } from "@steamfield/ui/reveal";
import { track } from "../lib/analytics";
import { type EventName, Events } from "../lib/events";

// The one module script every page loads. It isn't an island: docs pages ship no
// hydrated component, so copy buttons, section tracking and the TOC highlight
// are plain DOM code on @steamfield/ui's copy, docs and reveal cores. Each part does
// nothing on a page without its markup.

/** How long a copy button says "Copied". */
const COPIED_MS = 1500;

async function copyWithFeedback(button: HTMLButtonElement, read: () => Promise<string> | string) {
  const label = button.textContent;
  let ok = false;
  try {
    ok = await copyText(await read());
  } catch {
    // The Markdown fetch failed; the button says so below.
  }
  button.textContent = ok ? "Copied" : "Copy failed";
  setTimeout(() => {
    button.textContent = label;
  }, COPIED_MS);
}

/**
 * Adds a copy button to each code frame, in the markup enhanceCopyButtons wires: the
 * button names its code with aria-controls, and a status region beside it reports
 * the result.
 */
function setupCodeCopy() {
  let count = 0;
  for (const frame of document.querySelectorAll<HTMLElement>(".code-frame")) {
    const code = frame.querySelector("code");
    if (!code) continue;
    // A generated id must not collide with one already on the page, or the button
    // would copy that element instead.
    while (!code.id) {
      count += 1;
      const id = `code-frame-${count}`;
      if (!document.getElementById(id)) code.id = id;
    }
    const button = document.createElement("button");
    button.type = "button";
    button.className = "code-copy";
    button.dataset.copyCommand = "";
    button.setAttribute("aria-controls", code.id);
    const label = document.createElement("span");
    label.dataset.copyLabel = "";
    label.textContent = "Copy";
    button.append(label);
    const result = document.createElement("span");
    result.dataset.copyResult = "";
    result.dataset.part = "copy-result";
    result.setAttribute("role", "status");
    result.setAttribute("aria-live", "polite");
    frame.append(button, result);
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
  spyToc(document, { linkSelector: "[data-toc] a[href^='#']" });
}

/**
 * Copy buttons beside command lines (CopyLine.astro) and code frames: hidden until
 * JavaScript runs. A CopyLine's data-track names the event from the catalog, sent after
 * every attempt; data-track-method is its method.
 */
function setupCopyButtons() {
  const tracked = new Set<string>(Object.values(Events));
  enhanceCopyButtons(document, {
    copiedMs: COPIED_MS,
    onCopy: ({ id }) => {
      const button = document.querySelector<HTMLElement>(
        `[data-copy-command][aria-controls="${id}"]`,
      );
      const name = button?.dataset.track;
      if (name && tracked.has(name)) {
        const method = button.dataset.trackMethod;
        track(name as EventName, method ? { method } : undefined);
      }
    },
  });
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
 * Reveals each [data-reveal] section once (initReveal), as its top passes three
 * quarters of the way down the viewport. Without IntersectionObserver or with reduced
 * motion nothing is hidden. A revealed section is marked settled once its entrance
 * transitions end, so .lift surfaces get their own hover transition back.
 */
function setupReveals() {
  const settle = (el: Element) => {
    // getAnimations flushes styles, so the entrance transitions exist by now.
    const entrances = el
      .getAnimations({ subtree: true })
      .filter((animation) => animation instanceof CSSTransition);
    void Promise.allSettled(entrances.map((animation) => animation.finished)).then(() =>
      el.classList.add("is-settled"),
    );
  };
  new MutationObserver((records) => {
    for (const record of records) settle(record.target as Element);
  }).observe(document.body, { subtree: true, attributeFilter: ["data-revealed"] });
  initReveal({ rootMargin: "0px 0px -25% 0px" });
}

setupReveals();
setupCodeCopy();
setupCopyButtons();
setupSectionViews();
setupCommandTabTracking();
setupCopyPage();
setupTocHighlight();
