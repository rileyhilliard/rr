// @vitest-environment happy-dom
import type { PostHog } from "posthog-js";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as analytics from "./analytics";
import { Events } from "./events";

// The layout's text, read through Vite; empty (and failing below) if the file is missing.
const entry = import.meta.glob<string>("../layouts/BaseLayout.astro", {
  query: "?raw",
  import: "default",
  eager: true,
});
const entrySource = Object.values(entry)[0] ?? "";

type Init = (key: string, config: { loaded: (p: PostHog) => void }) => void;

// A stand-in for the SDK at its boundary: records calls, fires loaded on demand.
function fakeSdk() {
  const calls: unknown[][] = [];
  let loaded: (() => void) | undefined;
  const instance = {
    capture: (...args: unknown[]) => calls.push(["capture", ...args]),
    identify: (...args: unknown[]) => calls.push(["identify", ...args]),
    reset: () => calls.push(["reset"]),
  } as unknown as PostHog;
  const init: Init = (_key, config) => {
    loaded = () => config.loaded(instance);
  };
  const sdk = { ...instance, init } as unknown as PostHog;
  const load = vi.fn(async () => ({ default: sdk }));
  return { calls, load, fireLoaded: () => loaded?.() };
}

afterEach(() => {
  analytics.resetAnalyticsForTests();
  vi.restoreAllMocks();
});

describe("without a key", () => {
  it("never loads the SDK and every call is a silent no-op", async () => {
    const sdk = fakeSdk();
    const warn = vi.spyOn(console, "warn");
    await analytics.initAnalytics({ key: "" }, sdk.load);
    analytics.track(Events.sectionViewed);
    analytics.identify("user_1");
    analytics.reset();
    expect(sdk.load).not.toHaveBeenCalled();
    expect(sdk.calls).toEqual([]);
    expect(warn).not.toHaveBeenCalled();
  });
});

describe("with a key", () => {
  it("replays calls made before init in order", async () => {
    const sdk = fakeSdk();
    const init = analytics.initAnalytics({ key: "phc_test" }, sdk.load);
    analytics.track(Events.sectionViewed, { position: 1 });
    analytics.identify("user_1");
    await init;
    expect(sdk.calls).toEqual([]);
    sdk.fireLoaded();
    analytics.track(Events.sectionViewed, { position: 2 });
    analytics.reset();
    expect(sdk.calls).toEqual([
      ["capture", "section_viewed", { position: 1 }],
      ["identify", "user_1", undefined],
      ["capture", "section_viewed", { position: 2 }],
      ["reset"],
    ]);
  });

  it("drops calls past the buffer limit and warns once", async () => {
    const sdk = fakeSdk();
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    const init = analytics.initAnalytics({ key: "phc_test" }, sdk.load);
    for (let i = 0; i < 60; i++) analytics.track(Events.sectionViewed, { i });
    await init;
    sdk.fireLoaded();
    expect(sdk.calls).toHaveLength(50);
    expect(warn).toHaveBeenCalledTimes(1);
  });

  it("turns analytics off when the SDK fails to load", async () => {
    const blocked = vi.fn(() => Promise.reject(new Error("blocked")));
    const init = analytics.initAnalytics({ key: "phc_test" }, blocked);
    analytics.track(Events.sectionViewed, { position: 1 });
    await expect(init).resolves.toBeUndefined();
    analytics.track(Events.sectionViewed, { position: 2 });
    // A later init starts clean: nothing queued before the failure is replayed.
    const sdk = fakeSdk();
    await analytics.initAnalytics({ key: "phc_test" }, sdk.load);
    sdk.fireLoaded();
    expect(sdk.calls).toEqual([]);
  });

  it("initializes once across repeated calls", async () => {
    const sdk = fakeSdk();
    await Promise.all([
      analytics.initAnalytics({ key: "phc_test" }, sdk.load),
      analytics.initAnalytics({ key: "phc_test" }, sdk.load),
    ]);
    expect(sdk.load).toHaveBeenCalledTimes(1);
  });

  it("applies its config to the installed posthog-js", async () => {
    // The real SDK, kept offline: before_send sees each event and drops it.
    const { default: posthog } = await import("posthog-js");
    const seen: string[] = [];
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("offline"));
    const realInit = posthog.init.bind(posthog);
    // A spy, so afterEach's restoreAllMocks puts the real init back.
    vi.spyOn(posthog, "init").mockImplementation((key, config) =>
      realInit(key, {
        ...config,
        // The test DOM's user agent counts as a bot, and posthog-js drops
        // bot events before before_send. Without this, nothing is seen.
        opt_out_useragent_filter: true,
        advanced_disable_flags: true,
        before_send: (event) => {
          if (event) seen.push(event.event);
          return null;
        },
      }),
    );
    const load = async () => ({ default: posthog });
    const config = { key: "phc_test", host: "http://127.0.0.1:9" };
    await analytics.initAnalytics(config, load);
    analytics.track(Events.sectionViewed);
    expect(seen).toContain("section_viewed");
    expect(posthog.config.autocapture).toBe(false);
    expect(posthog.config.capture_pageview).toBe("history_change");
    expect(posthog.config.cookieless_mode).toBe("always");
    expect(posthog.config.disable_session_recording).toBe(true);
  });
});

// The module sends nothing until the layout calls it, so check that wiring.
describe("wiring", () => {
  it("BaseLayout.astro imports and calls initAnalytics", () => {
    const imported =
      /import\s*\{[^}]*\binitAnalytics\b[^}]*\}\s*from\s*["'][^"']*lib\/analytics["']/;
    expect(entrySource).toMatch(imported);
    expect(entrySource).toMatch(/\binitAnalytics\(/);
  });
});
