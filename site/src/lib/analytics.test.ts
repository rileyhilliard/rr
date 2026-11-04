// @vitest-environment happy-dom
import posthog, { type PostHog } from "posthog-js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createPostHogPipeline,
  type PostHogPipeline,
  TEST_DOM_OVERRIDES,
} from "../../tests/helpers/posthog-pipeline";
import { everythingOn } from "../../tests/helpers/posthog-remote-config";
import type * as AnalyticsModule from "./analytics";
import type { Consent, ConsentStore } from "./analytics";
import type { EventName } from "./events";

// A test-only event name, so these tests don't depend on the site's catalog.
const testEvent = "test_event_captured" as EventName;

// Persistence and stored consent are keyed by the token, so each test gets its own.
let keyCounter = 0;
let key = "";
let analytics: typeof AnalyticsModule;
let pipeline: PostHogPipeline;
let storedBefore: Set<string>;

function storageNames(): string[] {
  const cookies = document.cookie
    .split(";")
    .map((c) => c.split("=")[0]?.trim() ?? "")
    .filter((name) => name !== "")
    .map((name) => `cookie:${name}`);
  const local = Object.keys(localStorage).map((name) => `localStorage:${name}`);
  const session = Object.keys(sessionStorage).map((name) => `sessionStorage:${name}`);
  return [...cookies, ...local, ...session];
}

// Cookie, localStorage and sessionStorage names that appeared since the test started,
// after PostHog's debounced persistence save has been forced through.
function newStorage(live: PostHogPipeline = pipeline): string[] {
  live.posthog().persistence?.flush();
  live.posthog().sessionPersistence?.flush();
  return storageNames()
    .filter((name) => !storedBefore.has(name))
    .sort();
}

beforeEach(async () => {
  vi.resetModules();
  analytics = await import("./analytics");
  key = `phc_test_${++keyCounter}`;
  pipeline = createPostHogPipeline(posthog);
  storedBefore = new Set(storageNames());
});

afterEach(() => {
  vi.useRealTimers();
  pipeline.shutdown();
  localStorage.clear();
  sessionStorage.clear();
  vi.restoreAllMocks();
});

function start(config: Omit<AnalyticsModule.AnalyticsConfig, "key" | "site"> = {}) {
  return analytics.initAnalytics({ key, site: "rr", ...config }, pipeline.load);
}

// posthog-js captures the initial $pageview on a timer after loaded.
function firstPageview(live: PostHogPipeline = pipeline) {
  return vi.waitFor(() => live.only("$pageview"));
}

// The next page in the same tab: the first page unloads (keeping its storage), then a
// fresh module loads with the same key. On a sibling *.steamfield.io site the
// .steamfield.io cookies come along, but the other origin's web storage doesn't.
async function nextPage(consent: ConsentStore | undefined, { siblingSite = false } = {}) {
  pipeline.leavePage();
  if (siblingSite) {
    localStorage.clear();
    sessionStorage.clear();
  }
  vi.resetModules();
  analytics = await import("./analytics");
  const next = createPostHogPipeline(posthog);
  await analytics.initAnalytics({ key, site: "rr", consent }, next.load);
  return next;
}

// A minimal store with the shape @steamfield/ui/consent's createConsentStore returns.
function consentStore(initial: Consent = null) {
  let state = initial;
  const listeners = new Set<() => void>();
  const store: ConsentStore & { set(next: Consent): void } = {
    getState: () => state,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    set: (next) => {
      state = next;
      for (const listener of listeners) listener();
    },
  };
  return store;
}

describe("without a key", () => {
  it("never loads the SDK and every call is a silent no-op", async () => {
    const load = vi.fn(pipeline.load);
    const warn = vi.spyOn(console, "warn");
    await analytics.initAnalytics({ key: "", site: "rr", consent: consentStore("granted") }, load);
    analytics.track(testEvent);
    expect(load).not.toHaveBeenCalled();
    expect(pipeline.captured()).toEqual([]);
    expect(warn).not.toHaveBeenCalled();
  });
});

describe("before consent", () => {
  it("events captured before consent are cookieless and carry the site", async () => {
    const init = start({ consent: consentStore() });
    analytics.track(testEvent, { position: 1 });
    await init;
    const pageview = await firstPageview();
    expect(pageview.properties).toMatchObject({
      site: "rr",
      $cookieless_mode: true,
    });
    expect(pipeline.only(testEvent).properties).toMatchObject({
      site: "rr",
      position: 1,
      $cookieless_mode: true,
    });
  });

  it("stores nothing on the device before consent", async () => {
    await start({ consent: consentStore() });
    await firstPageview();
    analytics.track(testEvent);
    expect(newStorage()).toEqual([]);
  });

  it("stays cookieless without a consent store", async () => {
    await start();
    expect((await firstPageview()).properties).toMatchObject({
      site: "rr",
      $cookieless_mode: true,
    });
    expect(newStorage()).toEqual([]);
  });
});

describe("following the consent store", () => {
  it("events captured after accepting are not cookieless and keep the site", async () => {
    const store = consentStore();
    await start({ consent: store });
    await firstPageview();
    store.set("granted");
    pipeline.clear();
    analytics.track(testEvent);
    const event = pipeline.only(testEvent);
    expect(event.properties.site).toBe("rr");
    expect(event.properties.$cookieless_mode).not.toBe(true);
    expect(newStorage()).toEqual(
      [
        `cookie:ph_${key}_posthog`,
        `cookie:ph_${key}_posthog_cpm`,
        `cookie:__ph_opt_in_out_${key}`,
        `localStorage:ph_${key}_posthog`,
        `sessionStorage:ph_${key}_posthog`,
        `sessionStorage:ph_${key}_window_id`,
      ].sort(),
    );
  });

  it("events captured after accepting then resetting are cookieless and leave no new storage", async () => {
    const store = consentStore("granted");
    await start({ consent: store });
    await firstPageview();
    store.set(null);
    pipeline.clear();
    analytics.track(testEvent);
    expect(pipeline.only(testEvent).properties).toMatchObject({
      site: "rr",
      $cookieless_mode: true,
    });
    expect(newStorage()).toEqual([]);
  });

  it("events captured after accepting then declining are cookieless and leave no new storage", async () => {
    const store = consentStore("granted");
    await start({ consent: store });
    await firstPageview();
    store.set("denied");
    pipeline.clear();
    analytics.track(testEvent);
    expect(pipeline.only(testEvent).properties).toMatchObject({
      site: "rr",
      $cookieless_mode: true,
    });
    expect(newStorage()).toEqual([]);
  });

  it("a withdrawal with storage throwing still counts the next event cookieless", async () => {
    const store = consentStore("granted");
    await start({ consent: store });
    await firstPageview();
    const blocked = () => {
      throw new DOMException("blocked", "SecurityError");
    };
    const throwing = {
      getItem: blocked,
      setItem: blocked,
      removeItem: blocked,
      clear: blocked,
    };
    // Stubbed globals: happy-dom's Storage proxy can't have a method spied on and restored.
    vi.stubGlobal("sessionStorage", throwing);
    vi.stubGlobal("localStorage", throwing);
    try {
      pipeline.clear();
      expect(() => store.set("denied")).not.toThrow();
      analytics.track(testEvent);
      expect(pipeline.only(testEvent).properties).toMatchObject({
        site: "rr",
        $cookieless_mode: true,
      });
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("declining writes no consent record", async () => {
    const store = consentStore();
    await start({ consent: store });
    await firstPageview();
    store.set("denied");
    analytics.track(testEvent);
    expect(pipeline.only(testEvent).properties.$cookieless_mode).toBe(true);
    expect(newStorage()).toEqual([]);
  });
});

describe("a choice stored from an earlier visit", () => {
  it("a stored granted choice counts the first pageview with consent", async () => {
    vi.useFakeTimers();
    await start({ consent: consentStore("granted") });
    // Past posthog-js's deferred initial-pageview timer (1ms). Not runAllTimers: heatmaps,
    // on after Accept, poll on an interval that never runs out.
    await vi.advanceTimersByTimeAsync(100);
    const [pageview, ...more] = pipeline.captured("$pageview");
    expect(more).toEqual([]);
    expect(pageview?.properties.site).toBe("rr");
    expect(pageview?.properties.$cookieless_mode).not.toBe(true);
  });

  it("a stored granted choice opts in once, not again on the next page", async () => {
    await start({ consent: consentStore("granted") });
    await firstPageview();
    expect(pipeline.captured("$opt_in")).toHaveLength(1);
    const next = await nextPage(consentStore("granted"));
    const pageview = await firstPageview(next);
    expect(next.captured("$opt_in")).toEqual([]);
    expect(pageview.properties.$cookieless_mode).not.toBe(true);
    next.shutdown();
  });

  it("a PostHog grant on the device is cleared when the store is undecided", async () => {
    await start({ consent: consentStore("granted") });
    await firstPageview();
    expect(newStorage()).not.toEqual([]);
    // The next page: the visitor's sf-consent cookie is gone, PostHog's grant isn't.
    const next = await nextPage(consentStore(null));
    const pageview = await firstPageview(next);
    expect(pageview.properties).toMatchObject({
      site: "rr",
      $cookieless_mode: true,
    });
    expect(next.posthog().get_explicit_consent_status()).toBe("pending");
    expect(newStorage(next)).toEqual([]);
    next.shutdown();
  });

  it("without a store, a grant made on a sibling site is honored", async () => {
    // Nothing reconciles PostHog with sf-consent here, so its shared consent cookie decides.
    // BaseLayout always passes a store, which is what keeps sf-consent in charge.
    await start({ consent: consentStore("granted") });
    await firstPageview();
    const next = await nextPage(undefined, { siblingSite: true });
    const pageview = await firstPageview(next);
    expect(pageview.properties).toMatchObject({ site: "rr" });
    expect(pageview.properties.$cookieless_mode).not.toBe(true);
    next.shutdown();
  });

  it("a window id left by a tab withdrawn elsewhere is cleared on load", async () => {
    // Consent withdrawn in another tab: PostHog's records are gone, this tab's window id isn't.
    sessionStorage.setItem(`ph_${key}_window_id`, JSON.stringify("window-from-before"));
    await start({ consent: consentStore("denied") });
    await firstPageview();
    expect(newStorage()).toEqual([]);
  });

  it("a visitor who accepted on a sibling site keeps their identity here", async () => {
    await start({ consent: consentStore("granted") });
    const siblingId = (await firstPageview()).properties.distinct_id;
    const next = await nextPage(consentStore("granted"), { siblingSite: true });
    const pageview = await firstPageview(next);
    expect(pageview.properties.distinct_id).toBe(siblingId);
    expect(next.captured("$opt_in")).toEqual([]);
    next.shutdown();
  });
});

describe("init", () => {
  it("initializes with the module's options", async () => {
    await start({ host: "http://127.0.0.1:9" });
    const expected = {
      api_host: "http://127.0.0.1:9",
      defaults: "2026-08-30",
      cookieless_mode: "on_reject",
      opt_out_capturing_by_default: true,
      persistence: "localStorage+cookie",
      cross_subdomain_cookie: true,
      capture_pageview: "history_change",
      disable_capture_url_hashes: true,
      disable_session_recording: true,
      capture_dead_clicks: false,
      capture_heatmaps: false,
      capture_exceptions: false,
      disable_surveys: true,
      disable_conversations: true,
      disable_product_tours: true,
      opt_out_capturing_persistence_type: "cookie",
      autocapture: true,
      capture_performance: { web_vitals: true, network_timing: false },
      session_recording: {
        maskAllInputs: true,
        maskTextSelector: "[data-ph-mask]",
        recordHeaders: false,
        recordBody: false,
      },
      enable_recording_console_log: true,
      advanced_disable_feature_flags: true,
      advanced_disable_feature_flags_on_first_load: true,
    };
    expect(pipeline.initConfig()).toMatchObject(expected);
    expect(pipeline.initConfig()).not.toHaveProperty("advanced_disable_flags");
    expect(pipeline.initConfig().__extensionClasses).toEqual({
      logs: undefined,
    });
    // Scripts from PostHog's CDN (web vitals, the recorder) are allowed in production.
    expect(pipeline.initConfig()).not.toHaveProperty("disable_external_dependency_loading");
  });

  it("leaves only the test DOM's settings to the harness", async () => {
    await start();
    for (const name of Object.keys(TEST_DOM_OVERRIDES)) {
      expect(pipeline.initConfig()).not.toHaveProperty(name);
    }
  });

  it("requests remote config and never /flags", async () => {
    await start({ consent: consentStore("granted") });
    await firstPageview();
    expect(pipeline.scripts()).toContain("remote-config");
    expect(pipeline.requests().filter((url) => url.includes("/flags"))).toEqual([]);
  });

  it("never requests /flags when remote config says the project has flags", async () => {
    const live = createPostHogPipeline(posthog, {
      remoteConfig: { hasFeatureFlags: true },
    });
    await analytics.initAnalytics({ key, site: "rr", consent: consentStore() }, live.load);
    await firstPageview(live);
    expect(live.scripts()).toContain("remote-config");
    expect(live.requests().filter((url) => url.includes("/flags"))).toEqual([]);
    live.shutdown();
  });

  it("initializes once across repeated calls", async () => {
    const load = vi.fn(pipeline.load);
    await Promise.all([
      analytics.initAnalytics({ key, site: "rr" }, load),
      analytics.initAnalytics({ key, site: "rr" }, load),
    ]);
    expect(load).toHaveBeenCalledTimes(1);
  });
});

// A click posthog-js's heatmaps and autocapture both accept: a real element, coordinates.
function clickOn(element: Element) {
  element.dispatchEvent(new MouseEvent("click", { bubbles: true, clientX: 12, clientY: 34 }));
}

// Heatmaps buffer clicks and send them as one $$heatmap event when they flush, which
// they also do when the page unloads.
function flushHeatmaps() {
  window.dispatchEvent(new Event("beforeunload"));
}

// Elements without attributes: posthog-js walks an element's attributes with for...in, and
// happy-dom's NamedNodeMap yields entries that aren't attributes, so autocapture throws on
// any element that has one. Real browsers don't; the attribute-free page avoids it.
function page() {
  document.body.innerHTML = "<form><input /><button>Search</button></form>";
  const button = document.querySelector("button");
  const input = document.querySelector("input");
  if (!button || !input) throw new Error("the test page didn't render");
  input.value = "python api with a secret plan";
  return { button, input };
}

// happy-dom's settings for this window; the cast stays in this test.
type IframeSettings = {
  disableIframePageLoading: boolean;
  enableIframePageLoading: boolean;
};
function happyDOMSettings(): IframeSettings {
  return (window as unknown as { happyDOM: { settings: IframeSettings } }).happyDOM.settings;
}
const iframeDefaults = { ...happyDOMSettings() };

// The pageshow a browser fires for a page restored from the back-forward cache.
function restored() {
  const event = new Event("pageshow");
  Object.defineProperty(event, "persisted", { value: true });
  return event;
}

describe("capture features", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("autocapture counts clicks for a cookieless visitor without input values", async () => {
    const { button, input } = page();
    await start({ consent: consentStore() });
    await firstPageview();
    input.dispatchEvent(new Event("change", { bubbles: true }));
    clickOn(button);
    const clicks = pipeline.captured("$autocapture");
    expect(clicks.length).toBeGreaterThan(0);
    for (const click of clicks) {
      expect(click.properties).toMatchObject({
        site: "rr",
        $cookieless_mode: true,
      });
      expect(JSON.stringify(click.properties)).not.toContain("secret plan");
    }
    expect(newStorage()).toEqual([]);
  });

  it("sends page URLs without the hash, which carries search text", async () => {
    await start({ consent: consentStore() });
    await firstPageview();
    pipeline.clear();
    // posthog-js wraps history.pushState only once per window, for the first instance in
    // this file, so the navigation is announced the way Back would be: popstate, which
    // each instance listens for.
    history.pushState(null, "", "/search/#q=zebra");
    window.dispatchEvent(new PopStateEvent("popstate"));
    const pageview = await vi.waitFor(() => pipeline.only("$pageview"));
    expect(pageview.properties.$current_url).toBe(`${location.origin}/search/`);
    history.pushState(null, "", "/");
  });

  it("web vitals capture is on for every visitor", async () => {
    await start({ consent: consentStore() });
    expect(pipeline.posthog().webVitalsAutocapture?.isEnabled).toBe(true);
  });

  it("heatmaps stay off before consent", async () => {
    const { button } = page();
    await start({ consent: consentStore() });
    await firstPageview();
    clickOn(button);
    flushHeatmaps();
    expect(pipeline.captured("$$heatmap")).toEqual([]);
    expect(newStorage()).toEqual([]);
  });

  it("heatmaps and recording stay off without a consent store", async () => {
    // Nothing reconciles consent here, so this is the init options alone.
    const { button } = page();
    await start();
    await firstPageview();
    clickOn(button);
    flushHeatmaps();
    expect(pipeline.captured("$$heatmap")).toEqual([]);
    expect(pipeline.posthog().config.disable_session_recording).toBe(true);
  });

  it("heatmaps turn on after accepting, with the site", async () => {
    const { button } = page();
    const store = consentStore();
    await start({ consent: store });
    await firstPageview();
    store.set("granted");
    clickOn(button);
    flushHeatmaps();
    expect(pipeline.only("$$heatmap").properties).toMatchObject({
      site: "rr",
    });
  });

  it("heatmaps turn on for a stored grant", async () => {
    const { button } = page();
    await start({ consent: consentStore("granted") });
    await firstPageview();
    clickOn(button);
    flushHeatmaps();
    expect(pipeline.captured("$$heatmap")).toHaveLength(1);
  });

  it("heatmaps and recording turn on again on the next page after accepting", async () => {
    // PostHog already holds the grant there, so nothing changes its consent status.
    await start({ consent: consentStore("granted") });
    await firstPageview();
    const next = await nextPage(consentStore("granted"));
    await firstPageview(next);
    const { button } = page();
    clickOn(button);
    flushHeatmaps();
    expect(next.captured("$$heatmap")).toHaveLength(1);
    expect(next.posthog().config.disable_session_recording).toBe(false);
    next.shutdown();
  });

  it("heatmaps stop after a withdrawal", async () => {
    const { button } = page();
    const store = consentStore("granted");
    await start({ consent: store });
    await firstPageview();
    store.set("denied");
    clickOn(button);
    flushHeatmaps();
    expect(pipeline.captured("$$heatmap")).toEqual([]);
  });

  it("recording is switched on after accepting and off after a withdrawal", async () => {
    const store = consentStore();
    await start({ consent: store });
    await firstPageview();
    const recordingOff = () => pipeline.posthog().config.disable_session_recording;
    expect(recordingOff()).toBe(true);
    store.set("granted");
    expect(recordingOff()).toBe(false);
    store.set("denied");
    expect(recordingOff()).toBe(true);
    store.set("granted");
    store.set(null);
    expect(recordingOff()).toBe(true);
  });

  it("recording is switched on for a stored grant", async () => {
    await start({ consent: consentStore("granted") });
    expect(pipeline.posthog().config.disable_session_recording).toBe(false);
  });
});

describe("when the project's remote config turns everything on", () => {
  let live: PostHogPipeline;

  beforeEach(async () => {
    // rrweb's polyfill reads window.DOMTokenList, which happy-dom lists but leaves undefined.
    vi.stubGlobal("DOMTokenList", document.createElement("div").classList.constructor);
    // The recorder bundle, so posthog-js needn't fetch it from the CDN.
    await import("posthog-js/dist/posthog-recorder");
    // rrweb makes an about:blank iframe for clean copies of built-ins, which happy-dom
    // refuses (and reports) unless iframe page loading is on; about:blank fetches nothing.
    Object.assign(happyDOMSettings(), {
      disableIframePageLoading: false,
      enableIframePageLoading: true,
    });
    live = createPostHogPipeline(posthog, { remoteConfig: everythingOn });
  });

  afterEach(() => {
    live.shutdown();
    Object.assign(happyDOMSettings(), iframeDefaults);
  });

  async function startLive(consent: ConsentStore) {
    await analytics.initAnalytics({ key, site: "rr", consent }, live.load);
    await firstPageview(live);
    return live.posthog();
  }

  it("loads remote config as a script, as in production", async () => {
    await startLive(consentStore());
    expect(live.scripts()).toContain("remote-config");
    expect(live.requests().filter((url) => url.includes("/config"))).toEqual([]);
  });

  it("keeps heatmaps, recording and dead clicks off before consent", async () => {
    const sdk = await startLive(consentStore());
    const { button } = page();
    clickOn(button);
    flushHeatmaps();
    expect(live.captured("$$heatmap")).toEqual([]);
    expect(sdk.sessionRecordingStarted()).toBe(false);
    expect(sdk.deadClicksAutocapture?.isEnabled(sdk.deadClicksAutocapture)).toBe(false);
    expect(newStorage(live)).toEqual([]);
  });

  it("keeps exception capture, surveys, conversations, product tours and console logs off", async () => {
    const sdk = await startLive(consentStore("granted"));
    expect(sdk.exceptionObserver?.isEnabled).toBe(false);
    expect(sdk.deadClicksAutocapture?.isEnabled(sdk.deadClicksAutocapture)).toBe(false);
    // Each of these is a script from PostHog's CDN; none is even asked for.
    expect(
      live.scripts().filter((name) => /exception|surveys|conversations|tours|logs/.test(name)),
    ).toEqual([]);
    expect(
      live.requests().filter((url) => /surveys|conversations|product_tours|logs/.test(url)),
    ).toEqual([]);
  });

  it("starts recording for a stored grant", async () => {
    const sdk = await startLive(consentStore("granted"));
    expect(sdk.sessionRecordingStarted()).toBe(true);
  });

  it("starts recording after accepting", async () => {
    const store = consentStore();
    const sdk = await startLive(store);
    expect(sdk.sessionRecordingStarted()).toBe(false);
    store.set("granted");
    expect(sdk.sessionRecordingStarted()).toBe(true);
  });

  it("doesn't record a visitor who declined", async () => {
    const sdk = await startLive(consentStore("denied"));
    expect(sdk.sessionRecordingStarted()).toBe(false);
  });

  it("stops recording on a withdrawal", async () => {
    const store = consentStore("granted");
    const sdk = await startLive(store);
    store.set("denied");
    expect(sdk.sessionRecordingStarted()).toBe(false);
  });

  it("doesn't patch the console for Logs, before or after Accept", async () => {
    const original = window.console.log;
    const store = consentStore();
    await startLive(store);
    expect(window.console.log).toBe(original);
    store.set("granted");
    // After Accept the recorder wraps the console for console messages in replays, by
    // design; that wrapper is rrweb's, over the original, and nothing wraps it for Logs.
    const wrapped = window.console.log as typeof original & {
      __rrweb_original__?: unknown;
    };
    expect(wrapped === original || wrapped.__rrweb_original__ === original).toBe(true);
    expect(live.scripts().filter((name) => name.includes("logs"))).toEqual([]);
    expect(live.requests().filter((url) => url.includes("logs"))).toEqual([]);
  });

  it("drops heatmap data once PostHog's consent is gone, even with listeners attached", async () => {
    const sdk = await startLive(consentStore("granted"));
    // A decline on a sibling site clears PostHog's shared consent cookie; this page's
    // store and heatmap listeners don't know yet.
    sdk.consent.reset();
    const { button } = page();
    clickOn(button);
    flushHeatmaps();
    expect(live.captured("$$heatmap")).toEqual([]);
  });

  it("stops recording and heatmaps when a page comes back from the cache after a decline elsewhere", async () => {
    const sdk = await startLive(consentStore("granted"));
    expect(sdk.sessionRecordingStarted()).toBe(true);
    sdk.consent.reset();
    window.dispatchEvent(restored());
    expect(sdk.sessionRecordingStarted()).toBe(false);
    expect(sdk.heatmaps?.isEnabled).toBe(false);
    expect(sdk.config.disable_session_recording).toBe(true);
  });

  it("keeps recording when a page comes back from the cache still granted", async () => {
    const sdk = await startLive(consentStore("granted"));
    window.dispatchEvent(restored());
    expect(sdk.sessionRecordingStarted()).toBe(true);
  });

  it("keeps heatmaps on only after accepting", async () => {
    const store = consentStore();
    await startLive(store);
    store.set("granted");
    const { button } = page();
    clickOn(button);
    flushHeatmaps();
    expect(live.only("$$heatmap").properties).toMatchObject({
      site: "rr",
    });
  });
});

describe("posthog-js contract that beforeSend's site stamp works around", () => {
  // These pin the SDK behavior that makes analytics.ts stamp site in before_send.
  beforeEach(async () => {
    const { default: sdk } = await pipeline.load();
    sdk.init(key, {
      cookieless_mode: "on_reject",
      opt_out_capturing_by_default: true,
      persistence: "localStorage+cookie",
      opt_out_capturing_persistence_type: "cookie",
    });
  });

  it("loses properties registered before opt_in_capturing", () => {
    pipeline.posthog().register({ site: "rr" });
    pipeline.posthog().opt_in_capturing();

    pipeline.posthog().capture("example_viewed");

    expect(pipeline.only("example_viewed").properties.site).toBeUndefined();
  });

  it("keeps properties registered after opt_in_capturing", () => {
    pipeline.posthog().opt_in_capturing();
    pipeline.posthog().register({ site: "rr" });

    pipeline.posthog().capture("example_viewed");

    expect(pipeline.only("example_viewed").properties.site).toBe("rr");
  });
});

// A stand-in for the SDK, for what the real one can't be made to do on cue:
// load late (so the queue fills) or fail to load.
function fakeSdk() {
  const calls: unknown[][] = [];
  let loaded: (() => void) | undefined;
  const instance = {
    capture: (...args: unknown[]) => calls.push(["capture", ...args]),
  } as unknown as PostHog;
  const init = (_key: string, config: { loaded: (p: PostHog) => void }) => {
    loaded = () => config.loaded(instance);
    return instance;
  };
  const load = vi.fn(async () => ({ default: { init } as unknown as PostHog }));
  return { calls, load, fireLoaded: () => loaded?.() };
}

describe("the queue", () => {
  it("keeps the first 50 calls made before init, drops the rest and warns once", async () => {
    const sdk = fakeSdk();
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    const init = analytics.initAnalytics({ key, site: "rr" }, sdk.load);
    for (let i = 0; i < 60; i++) analytics.track(testEvent, { i });
    await init;
    sdk.fireLoaded();
    expect(sdk.calls).toHaveLength(50);
    expect(sdk.calls.at(-1)).toEqual(["capture", testEvent, { i: 49 }]);
    expect(warn).toHaveBeenCalledTimes(1);
  });

  it("turns analytics off when the SDK fails to load", async () => {
    const blocked = vi.fn(() => Promise.reject(new Error("blocked")));
    const init = analytics.initAnalytics({ key, site: "rr" }, blocked);
    analytics.track(testEvent, { position: 1 });
    await expect(init).resolves.toBeUndefined();
    analytics.track(testEvent, { position: 2 });
    // A later init starts clean: nothing queued before the failure is replayed.
    const sdk = fakeSdk();
    await analytics.initAnalytics({ key, site: "rr" }, sdk.load);
    sdk.fireLoaded();
    expect(sdk.calls).toEqual([]);
  });
});
