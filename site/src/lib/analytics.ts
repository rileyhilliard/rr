// The only module that imports posthog-js. Analytics is off, and the SDK never
// loads, while the key is empty: the normal state in dev, tests, CI and sandboxes.
// The caller reads env and passes it in, so this module never touches import.meta.env.
//
// Consent: the visitor's banner choice (the sf-consent cookie, read through a consent
// store) is the single source of truth, and PostHog's own consent state follows it.
// Until the visitor accepts, PostHog runs cookieless and stores nothing on the device.
import type { BeforeSendFn, PostHog, PostHogConfig } from "posthog-js";
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
  // Without a store nothing reconciles PostHog with sf-consent, so PostHog's own consent
  // record decides: cookieless, unless a grant made on a sibling steamfield.io site is in
  // the shared .steamfield.io cookie, which it honors. BaseLayout always passes a store
  // when there's a key, so on the site the visitor's sf-consent choice is what applies.
  consent?: ConsentStore | undefined;
};
type Loader = () => Promise<{ default: Pick<PostHog, "init"> }>;
type Client = Pick<
  PostHog,
  | "capture"
  | "opt_in_capturing"
  | "clear_opt_in_out_capturing"
  | "get_explicit_consent_status"
  | "set_config"
  | "startSessionRecording"
  | "stopSessionRecording"
>;
type Call = (client: Client) => void;

// Calls made before the SDK loads wait here: a track() from an island that hydrates
// before posthog-js has loaded. stacks, rr, decks and statglass all call track().
const QUEUE_LIMIT = 50;
let client: Client | undefined;
let queue: Call[] | undefined;
let warned = false;

function send(call: Call) {
  if (client) return call(client);
  if (!queue) return; // Not initialized, or no key: a silent no-op.
  // Past the limit, calls are dropped, with one warning: unlike stacks, these sites
  // allow the console.
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
//
// Session recording and heatmaps run only after Accept, so they follow the same choice:
// switched on for granted (on every load, not only when PostHog's status changes, since
// both start off at init) and off otherwise. Heatmaps toggle through set_config, which
// makes posthog-js add or remove its listeners; recording through its start and stop calls.
function applyConsent(c: Client, consent: Consent, key: string) {
  const status = c.get_explicit_consent_status();
  if (consent === "granted") {
    if (status !== "granted") c.opt_in_capturing();
    c.set_config({ capture_heatmaps: true });
    c.startSessionRecording();
    return;
  }
  c.stopSessionRecording();
  c.set_config({ capture_heatmaps: false });
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

// Events that exist only after Accept. posthog-js's heatmaps don't check consent once
// their listeners are attached, so this is a floor under applyConsent: a page restored
// from the back-forward cache after a decline on a sibling site, before the pageshow
// handler below runs, still sends none of them.
const CONSENTED_ONLY = new Set(["$$heatmap", "$dead_click", "$snapshot"]);

// A registered super property isn't enough for site: opt_in_capturing() resets super
// properties and captures the initial $pageview (and $opt_in) before it returns, so
// those events would go out without it. Stamping each event as it leaves covers them.
function beforeSend(site: string): BeforeSendFn {
  return (event) => {
    if (!event) return null;
    const granted = client?.get_explicit_consent_status() === "granted";
    if (CONSENTED_ONLY.has(event.event) && !granted) return null;
    return { ...event, properties: { ...event.properties, site } };
  };
}

// A page restored from the back-forward cache keeps its consent store's old state, but
// PostHog's consent record is the shared .steamfield.io cookie, which a decline on a
// sibling site has cleared. Re-read it, and withdraw here too if it's no longer granted.
function onRestore(c: Client, key: string) {
  window.addEventListener("pageshow", (event) => {
    const granted = c.get_explicit_consent_status() === "granted";
    if (!event.persisted || granted) return;
    c.stopSessionRecording();
    c.set_config({ capture_heatmaps: false });
    c.clear_opt_in_out_capturing();
    forgetWindow(key);
  });
}

function onLoaded(instance: Client, key: string, store?: ConsentStore) {
  // Runs before posthog-js captures the deferred initial $pageview, so that
  // pageview already reflects the stored choice.
  if (store) {
    applyConsent(instance, store.getState(), key);
    store.subscribe(() => send((c) => applyConsent(c, store.getState(), key)));
  }
  onRestore(instance, key);
  client = instance;
  for (const call of queue ?? []) call(instance);
  queue = undefined;
}

// posthog-js spreads this over its defaults, so the logs key must be present and
// undefined. The cast is for statglass, whose exactOptionalPropertyTypes rejects an
// explicit undefined in this type.
type Extensions = NonNullable<PostHogConfig["__extensionClasses"]>;
const withoutLogs = { logs: undefined } as unknown as Extensions;

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
    // URLs without their hash: a hash such as stacks' /search/#q=… would put the search
    // text on every event.
    disable_capture_url_hashes: true,
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
    // For every visitor, cookieless included: clicks and form submissions (autocapture,
    // which never sends input values unless an element opts in with data-ph-capture-value),
    // and web vitals. Network timing is a recording feature and stays off, because request
    // URLs carry search text.
    autocapture: true,
    capture_performance: { web_vitals: true, network_timing: false },
    // After Accept only: applyConsent starts these. Recordings mask every input, and the
    // text of anything marked data-ph-mask (each site's AGENTS.md lists its masked elements).
    disable_session_recording: true,
    capture_heatmaps: false,
    // Remote config can turn on network payload capture; an explicit client false wins.
    session_recording: {
      maskAllInputs: true,
      maskTextSelector: "[data-ph-mask]",
      recordHeaders: false,
      recordBody: false,
    },
    enable_recording_console_log: true,
    // Off here rather than left to the project's remote config, so turning one on is a
    // code change.
    capture_dead_clicks: false,
    capture_exceptions: false,
    disable_surveys: true,
    disable_conversations: true,
    disable_product_tours: true,
    // The one remote-config switch with no client override in posthog-js 1.445.4: a
    // project with Logs' console capture on would patch the console and ship its output
    // for every visitor, cookieless included. Leaving the logs extension out of init
    // keeps that off; posthog.logs isn't used here.
    __extensionClasses: withoutLogs,
    // Remote config loads on every page (posthog-js starts a recording only when it says
    // replay is on); the options above win over what it switches on, except where
    // noted. Feature flags stay off, so /flags is never requested. Scripts from
    // PostHog's CDN (remote config, web vitals, the recorder) are allowed.
    advanced_disable_feature_flags: true,
    advanced_disable_feature_flags_on_first_load: true,
    before_send: beforeSend(config.site),
    loaded: (instance) => onLoaded(instance, key, config.consent),
  });
}

export function track(event: EventName, props?: EventProps): void {
  send((c) => c.capture(event, props));
}
