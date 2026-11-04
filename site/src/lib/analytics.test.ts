// @vitest-environment happy-dom
import posthog, { type PostHog } from "posthog-js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createPostHogPipeline,
  OFFLINE_OVERRIDES,
  type PostHogPipeline,
} from "../../tests/helpers/posthog-pipeline";
import type * as AnalyticsModule from "./analytics";
import type { Consent, ConsentStore } from "./analytics";
import type { EventName } from "./events";

// The catalog has no events yet; a test-only name stands in for one.
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
    // Fires posthog-js's deferred initial-pageview timer, and anything after it.
    await vi.runAllTimersAsync();
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
  it("initializes cookieless until consent, with remote-config features off", async () => {
    await start({ host: "http://127.0.0.1:9" });
    const expected = {
      api_host: "http://127.0.0.1:9",
      defaults: "2026-08-30",
      cookieless_mode: "on_reject",
      opt_out_capturing_by_default: true,
      persistence: "localStorage+cookie",
      cross_subdomain_cookie: true,
      capture_pageview: "history_change",
      autocapture: false,
      disable_session_recording: true,
      capture_dead_clicks: false,
      capture_heatmaps: false,
      capture_exceptions: false,
      capture_performance: false,
      disable_surveys: true,
      disable_conversations: true,
      disable_product_tours: true,
      opt_out_capturing_persistence_type: "cookie",
      advanced_disable_flags: true,
      disable_external_dependency_loading: true,
    };
    expect(pipeline.initConfig()).toMatchObject(expected);
  });

  it("leaves only the test DOM's user-agent setting to the harness", async () => {
    await start();
    // Production sets the harness's network keys itself (see the init options test).
    const harnessOnly = Object.keys(OFFLINE_OVERRIDES).filter(
      (name) => !["advanced_disable_flags", "disable_external_dependency_loading"].includes(name),
    );
    expect(harnessOnly).toEqual(["opt_out_useragent_filter"]);
    expect(pipeline.initConfig()).not.toHaveProperty("opt_out_useragent_filter");
  });

  it("requests no flags or remote config and loads no script from PostHog", async () => {
    // The SDK's own transport: every request it makes goes through _send_request.
    const sdk: PostHog = Object.getPrototypeOf(posthog);
    const requested: string[] = [];
    vi.spyOn(sdk, "_send_request").mockImplementation((options) => {
      requested.push(options.url);
    });
    const scriptsBefore = document.scripts.length;
    const live = createPostHogPipeline(posthog, { network: "as-configured" });
    await analytics.initAnalytics(
      {
        key,
        host: "http://127.0.0.1:9",
        site: "rr",
        consent: consentStore(),
      },
      live.load,
    );
    const pageview = await firstPageview(live);
    expect(pageview.properties).toMatchObject({
      site: "rr",
      $cookieless_mode: true,
    });
    expect(requested.filter((url) => /\/flags|\/config/.test(url))).toEqual([]);
    expect(document.scripts.length).toBe(scriptsBefore);
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

describe("posthog-js contract that stampSite works around", () => {
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
