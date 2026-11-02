// The only module that imports posthog-js. Analytics is off, and the SDK never
// loads, while the key is empty: the normal state in dev, tests, CI and sandboxes.
// The caller reads env and passes it in, so this module never touches import.meta.env.
import type { PostHog, PostHogConfig } from "posthog-js";
import type { EventName, EventProps } from "./events";

export type AnalyticsConfig = {
  key: string | undefined;
  // Empty means PostHog US cloud. Behind a reverse proxy, host is the proxy and
  // uiHost is the PostHog app (https://us.posthog.com or https://eu.posthog.com).
  host?: string | undefined;
  uiHost?: string | undefined;
  // The decisions live here as defaults; change them here, not at call sites.
  cookieless?: PostHogConfig["cookieless_mode"];
  autocapture?: boolean;
  recordSessions?: boolean;
};
type Loader = () => Promise<{ default: PostHog }>;
type Client = Pick<PostHog, "capture" | "identify" | "reset">;
type Call = (client: Client) => void;

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

export async function initAnalytics(
  config: AnalyticsConfig,
  load: Loader = () => import("posthog-js"),
): Promise<void> {
  if (!config.key || typeof window === "undefined" || queue || client) return;
  queue = [];
  try {
    const { default: posthog } = await load();
    posthog.init(config.key, {
      api_host: config.host || "https://us.i.posthog.com",
      ...(config.uiHost ? { ui_host: config.uiHost } : {}),
      // Explicit options below win over the defaults snapshot.
      defaults: "latest",
      person_profiles: "identified_only",
      capture_pageview: "history_change",
      cookieless_mode: config.cookieless ?? "always",
      autocapture: config.autocapture ?? false,
      disable_session_recording: !(config.recordSessions ?? false),
      loaded: (instance) => {
        client = instance;
        for (const call of queue ?? []) call(instance);
        queue = undefined;
      },
    });
  } catch {
    // An ad blocker or a network failure stopped the SDK: drop the queue and
    // leave analytics off, so later calls are no-ops and nothing rejects.
    queue = undefined;
  }
}

export function track(event: EventName, props?: EventProps): void {
  send((c) => c.capture(event, props));
}

// Identify by internal id only, after sign-in.
export function identify(id: string, traits?: EventProps): void {
  send((c) => c.identify(id, traits));
}

// Call on sign-out.
export function reset(): void {
  send((c) => c.reset());
}

// Tests only: forget the client and the queue between cases.
export function resetAnalyticsForTests(): void {
  client = undefined;
  queue = undefined;
  warned = false;
}
