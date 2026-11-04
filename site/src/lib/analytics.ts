// The only module that imports posthog-js. Analytics is off, and the SDK never
// loads, while the key is empty: the normal state in dev, tests, CI and sandboxes.
// The caller reads env and passes it in, so this module never touches import.meta.env.
//
// Consent: the visitor's banner choice (the sf-consent cookie, read through a consent
// store) is the single source of truth, and PostHog's own consent state follows it.
// Until the visitor accepts, PostHog runs cookieless and stores nothing on the device.
import type { BeforeSendFn, PostHog } from "posthog-js";
import type { EventName, EventProps } from "./events";

export type Consent = "granted" | "denied" | null;

// The shape of @steamfield/ui/consent's store, typed here so this module doesn't
// import the UI package. subscribe calls the listener after every change in this tab.
// The store isn't expected to sync across tabs: a choice made in another tab reaches
// this one on its next page load, which also clears what this tab's PostHog left behind.
export type ConsentStore = {
  getState(): Consent;
  subscribe(listener: () => void): () => void;
};

export type AnalyticsConfig = {
  key: string | undefined;
  // Empty means PostHog US cloud.
  host?: string | undefined;
  // Added as `site` to every event, so one PostHog project can tell the sites apart.
  site: string;
  // Without a store the visitor stays undecided, so PostHog stays cookieless.
  consent?: ConsentStore | undefined;
};
type Loader = () => Promise<{ default: Pick<PostHog, "init"> }>;
type Consenting = "opt_in_capturing" | "clear_opt_in_out_capturing";
type Status = "get_explicit_consent_status";
type Client = Pick<PostHog, "capture" | Consenting | Status>;
type Call = (client: Client) => void;

// Calls made before the SDK loads wait here. stacks only sends pageviews, but rr, decks
// and statglass share this module and call track() early.
const QUEUE_LIMIT = 50;
let client: Client | undefined;
let queue: Call[] | undefined;
let warned = false;

function send(call: Call) {
  if (client) return call(client);
  if (!queue) return; // Not initialized, or no key: a silent no-op.
  if (queue.length >= QUEUE_LIMIT) {
    if (!warned) console.warn("analytics: queue full, dropping calls");
    warned = true;
    return;
  }
  queue.push(call);
}

// Make PostHog's consent match the visitor's choice. Granted opts in (persistence on).
// Undecided and declined both go back to PostHog's pending state, which, with
// opt_out_capturing_by_default, counts as rejected: persistence off, its cookie,
// localStorage and sessionStorage entries removed (the window id by forgetWindow),
// and the next event cookieless. Declining stores nothing, not even PostHog's own
// opt-out record.
function applyConsent(c: Client, consent: Consent, key: string) {
  const status = c.get_explicit_consent_status();
  if (consent === "granted") {
    if (status !== "granted") c.opt_in_capturing();
    return;
  }
  if (status !== "pending") c.clear_opt_in_out_capturing();
  // Even when PostHog is already pending: consent withdrawn in another tab leaves
  // this tab's window id behind until a page here loads.
  forgetWindow(key);
}

// posthog-js 1.445 keeps the tab's window id in sessionStorage after consent is
// withdrawn (its session manager is torn down without removing it), so remove it
// here. The names follow posthog-js's default persistence name, the project key; the
// withdrawal tests in analytics.test.ts fail if a posthog-js upgrade changes them.
function forgetWindow(key: string) {
  try {
    sessionStorage.removeItem(`ph_${key}_window_id`);
    sessionStorage.removeItem(`ph_${key}_primary_window_exists`);
  } catch {
    // Storage is blocked, so posthog-js couldn't have written these either.
  }
}

// A registered super property isn't enough for site: opt_in_capturing() resets super
// properties and captures the initial $pageview (and $opt_in) before it returns, so
// those events would go out without it. Stamping each event as it leaves covers them.
function stampSite(site: string): BeforeSendFn {
  return (event) => {
    if (!event) return event;
    return { ...event, properties: { ...event.properties, site } };
  };
}

function onLoaded(instance: Client, key: string, store?: ConsentStore) {
  // Runs before posthog-js captures the deferred initial $pageview, so that
  // pageview already reflects the stored choice.
  if (store) {
    applyConsent(instance, store.getState(), key);
    store.subscribe(() => send((c) => applyConsent(c, store.getState(), key)));
  }
  client = instance;
  for (const call of queue ?? []) call(instance);
  queue = undefined;
}

export async function initAnalytics(
  config: AnalyticsConfig,
  load: Loader = () => import("posthog-js"),
): Promise<void> {
  const { key } = config;
  if (!key || typeof window === "undefined" || queue || client) return;
  queue = [];
  let posthog: Pick<PostHog, "init">;
  try {
    ({ default: posthog } = await load());
  } catch {
    // An ad blocker or a network failure stopped the SDK: drop the queue and
    // leave analytics off, so later calls are no-ops and nothing rejects.
    queue = undefined;
    return;
  }
  posthog.init(key, {
    api_host: config.host || "https://us.i.posthog.com",
    // The newest dated snapshot posthog-js 1.445 knows; the options below win over it.
    // Move it on purpose when upgrading, after reading what the new snapshot changes.
    defaults: "2026-08-30",
    person_profiles: "identified_only",
    capture_pageview: "history_change",
    // Cookieless until the visitor accepts; then localStorage+cookie, shared across
    // steamfield.io subdomains.
    cookieless_mode: "on_reject",
    opt_out_capturing_by_default: true,
    persistence: "localStorage+cookie",
    cross_subdomain_cookie: true,
    // PostHog's consent record goes in a cookie on .steamfield.io beside the identity
    // cookie. In per-origin localStorage, a visitor who accepted on a sibling site would
    // be opted in again here, and opting in resets them to a new distinct_id. Every
    // site on this key needs the same setting.
    opt_out_capturing_persistence_type: "cookie",
    // Pageviews and catalog events only. These are off here rather than left to the
    // project's remote config, so turning one on is a code change.
    autocapture: false,
    disable_session_recording: true,
    capture_dead_clicks: false,
    capture_heatmaps: false,
    capture_exceptions: false,
    capture_performance: false,
    disable_surveys: true,
    disable_conversations: true,
    disable_product_tours: true,
    before_send: stampSite(config.site),
    loaded: (instance) => onLoaded(instance, key, config.consent),
  });
}

export function track(event: EventName, props?: EventProps): void {
  send((c) => c.capture(event, props));
}
