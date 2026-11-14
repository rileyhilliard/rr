// Characterization of the page script: copy buttons, analytics hooks, scroll reveals and the
// TOC highlight. site.ts sets everything up when it is imported, so each test builds the DOM
// first and then imports a fresh copy of the module.
import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { stubIntersectionObserver } from "../../tests/helpers/dom";
import { renderDocsMarkdown } from "../../tests/helpers/markdown";

const track = vi.fn();
vi.mock("../lib/analytics", () => ({ track: (...args: unknown[]) => track(...args) }));

let intersect: ReturnType<typeof stubIntersectionObserver>["intersect"];

/** The reveal marker: html.js plus .is-visible before the migration, data-revealed after it. */
const motionGate = () =>
  document.documentElement.classList.contains("js") ||
  document.documentElement.hasAttribute("data-motion-ready");
const revealed = (el: Element) =>
  el.classList.contains("is-visible") || el.hasAttribute("data-revealed");

async function load(html: string) {
  document.body.innerHTML = html;
  vi.resetModules();
  await import("./site");
}

// site.ts adds window listeners each time it's imported; they're removed after each test so
// one test's listeners don't fire in the next.
let windowListeners: Parameters<typeof window.removeEventListener>[] = [];
let addWindowListener: ReturnType<typeof vi.spyOn> | undefined;

beforeEach(() => {
  const add = window.addEventListener.bind(window);
  addWindowListener = vi
    .spyOn(window, "addEventListener")
    .mockImplementation((...args: Parameters<typeof window.addEventListener>) => {
      windowListeners.push([args[0], args[1], args[2]]);
      add(...args);
    });
  track.mockClear();
  ({ intersect } = stubIntersectionObserver());
});

afterEach(() => {
  addWindowListener?.mockRestore();
  for (const args of windowListeners) window.removeEventListener(...args);
  windowListeners = [];
  vi.useRealTimers();
  vi.unstubAllGlobals();
  document.body.innerHTML = "";
  document.documentElement.className = "";
  for (const name of [...document.documentElement.getAttributeNames()]) {
    if (name.startsWith("data-")) document.documentElement.removeAttribute(name);
  }
});

const copyLine = (command: string, extra = "") => `
  <div>
    <span aria-hidden="true">$</span>
    <code id="c-${command.replace(/[^a-z0-9]+/gi, "-")}">${command}</code>
    <button type="button" hidden data-copy-command aria-controls="c-${command.replace(/[^a-z0-9]+/gi, "-")}" ${extra} aria-label="Copy ${command}"><span data-copy-label>Copy</span></button><span data-copy-result data-part="copy-result" role="status" aria-live="polite"></span>
  </div>`;

describe("copy lines (CopyLine.astro)", () => {
  it("shows the hidden button and copies the command, then resets the label", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    await load(copyLine("brew install rr"));
    const button = document.querySelector("button") as HTMLButtonElement;
    expect(button.hidden).toBe(false);
    await user.click(button);
    expect(await navigator.clipboard.readText()).toBe("brew install rr");
    expect(button).toHaveTextContent("Copied");
    await act(() => vi.advanceTimersByTimeAsync(1600));
    expect(button).toHaveTextContent("Copy");
    expect(button).not.toHaveTextContent("Copied");
  });

  it("tracks catalogued events with their method, and ignores unknown ones", async () => {
    const user = userEvent.setup();
    await load(
      copyLine("brew install rr", 'data-track="install_copied" data-track-method="Homebrew"') +
        copyLine("/rr:setup", 'data-track="plugin_copied"') +
        copyLine("rr doctor", 'data-track="not_an_event"'),
    );
    const buttons = [...document.querySelectorAll("button")];
    for (const button of buttons) await user.click(button);
    expect(track.mock.calls).toEqual([
      ["install_copied", { method: "Homebrew" }],
      ["plugin_copied", undefined],
    ]);
  });
});

describe("code frames", () => {
  it("get a copy button that copies the code", async () => {
    const user = userEvent.setup();
    // The markup the docs pages ship, from the same processor and plugins.
    const { code } = await renderDocsMarkdown('```yaml title=".rr.yaml"\nhosts: [m4]\n```\n');
    await load(code);
    const button = screen.getByRole("button", { name: "Copy code" }) as HTMLButtonElement;
    expect(button).toHaveTextContent("Copy");
    expect(button.hidden).toBe(false);
    await user.click(button);
    expect(await navigator.clipboard.readText()).toBe("hosts: [m4]");
    expect(button).toHaveTextContent("Copied");
  });
});

describe("analytics hooks", () => {
  it("tracks command tab switches announced by the island", async () => {
    await load("");
    window.dispatchEvent(new CustomEvent("rr:command-tab", { detail: { name: "doctor" } }));
    expect(track.mock.calls).toEqual([["command_tab_opened", { name: "doctor" }]]);
  });

  it("tracks a section view once", async () => {
    await load('<section id="agent" data-track-view></section>');
    const section = document.getElementById("agent") as HTMLElement;
    intersect(section);
    intersect(section);
    expect(track.mock.calls).toEqual([["section_viewed", { id: "agent" }]]);
  });
});

describe("scroll reveals", () => {
  it("gate the page for motion and reveal a section once it scrolls in", async () => {
    await load('<section id="a" data-reveal><p data-motion="lift-md">Hi</p></section>');
    const section = document.getElementById("a") as HTMLElement;
    expect(motionGate()).toBe(true);
    expect(revealed(section)).toBe(false);
    intersect(section);
    expect(revealed(section)).toBe(true);
  });

  it("leave everything visible without IntersectionObserver", async () => {
    vi.stubGlobal("IntersectionObserver", undefined);
    await load('<section id="a" data-reveal><p data-motion="lift-md">Hi</p></section>');
    expect(motionGate()).toBe(false);
  });
});

describe("table of contents", () => {
  it("marks the link of the heading being read", async () => {
    await load(`
      <nav data-toc><a href="#one">One</a><a href="#two">Two</a></nav>
      <h2 id="one">One</h2><h2 id="two">Two</h2>`);
    const [one, two] = [...document.querySelectorAll("[data-toc] a")];
    intersect(document.getElementById("two") as HTMLElement);
    expect(two).toHaveAttribute("aria-current", "location");
    expect(one).not.toHaveAttribute("aria-current");
  });
});
