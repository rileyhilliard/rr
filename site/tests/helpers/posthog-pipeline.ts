// Test-only: used by src/lib/analytics.test.ts. Adapted from OpenData's
// shared/testing/posthog-pipeline.ts.
import type { CaptureResult, PostHog, PostHogConfig } from "posthog-js";

/**
 * Boots the **real** posthog-js offline, through analytics.ts's `load` seam.
 *
 * This is the only layer that can honestly assert what ships. A fake SDK proves our
 * code calls `capture(...)`; it cannot prove anything about the payload that leaves
 * the browser, because posthog-js, not our code, adds `$cookieless_mode` and runs
 * `before_send` over the finished object.
 *
 * The wrapper changes the config analytics.ts passes to init in two ways, and only
 * these two:
 *
 * - It composes `before_send`: the module's own `before_send` (which stamps `site`)
 *   runs first, its output is recorded, and the wrapper then returns `null` so nothing
 *   is sent. `posthog.on("eventCaptured")` would see the same object, but only by
 *   letting the event reach the transport, and a real fetch per capture leaves
 *   requests in flight when vitest tears the happy-dom window down.
 * - It sets the OFFLINE_OVERRIDES keys below, overriding whatever the module passed
 *   for them. `initConfig()` returns the module's config before both changes, so a
 *   test can check that production doesn't set those keys itself.
 */

/** A fully enriched event as `before_send` sees it. */
export type CapturedEvent = CaptureResult;

type InitConfig = Partial<PostHogConfig>;
type PostHogModule = Pick<PostHog, "init">;
type BeforeSend = PostHogConfig["before_send"];
type BeforeSendFn = Exclude<BeforeSend, unknown[] | undefined>;

/** Config that keeps the real SDK offline and synchronous. */
export const OFFLINE_OVERRIDES = {
  /** No remote-config or recorder script fetches. */
  disable_external_dependency_loading: true,
  /** No `/flags` round trip on init. */
  advanced_disable_flags: true,
  /** happy-dom reports an automated user agent; without this everything drops. */
  opt_out_useragent_filter: true,
} as const;

export type PostHogPipeline = {
  /** Pass as initAnalytics' `load`: the real module, with init wrapped. */
  load: () => Promise<{ default: PostHogModule }>;
  /** The live instance init returned. Throws before init has run. */
  posthog(): PostHog;
  /** The config the caller passed to init, before the wrapper changed it. */
  initConfig(): InitConfig;
  /** Post-`before_send` payloads, in capture order, optionally by name. */
  captured(name?: string): CapturedEvent[];
  /**
   * The one event with this name, or a throw. Indexing `captured(name)[0]` both
   * fails `noUncheckedIndexedAccess` and turns "the event was dropped" into
   * "cannot read properties of undefined".
   */
  only(name: string): CapturedEvent;
  /** Drop everything recorded so far. */
  clear(): void;
  /** Clear persistence and stop recording. Call in `afterEach`. */
  shutdown(): void;
};

let instanceCounter = 0;

function runBeforeSend(hook: BeforeSend, event: CaptureResult | null) {
  if (hook === undefined) return event;
  const hooks: BeforeSendFn[] = Array.isArray(hook) ? hook : [hook];
  let current = event;
  for (const fn of hooks) current = current ? fn(current) : null;
  return current;
}

/**
 * An isolated, named PostHog instance, so tests cannot leak super properties or
 * opt-in state into one another through posthog-js's singleton. Persistence and the
 * stored consent are keyed by the token, so give each test its own key unless it
 * means to share them.
 */
export function createPostHogPipeline(posthog: PostHogModule): PostHogPipeline {
  const name = `pipeline_test_${++instanceCounter}`;
  const captured: CapturedEvent[] = [];
  let recording = true;
  let client: PostHog | undefined;
  let received: InitConfig | undefined;

  const wrapped: PostHogModule = {
    init: (token, config) => {
      received = config;
      client = posthog.init(
        token,
        {
          ...config,
          before_send: (event) => {
            const result = runBeforeSend(config?.before_send, event);
            if (result && recording) captured.push(result);
            // Never send. See the note on network teardown above.
            return null;
          },
          ...OFFLINE_OVERRIDES,
        },
        name,
      );
      return client;
    },
  };

  const named = (event?: string) =>
    captured.filter((e) => event === undefined || e.event === event);

  const live = () => {
    if (!client) throw new Error(`posthog.init has not run for ${name}`);
    return client;
  };

  return {
    load: async () => ({ default: wrapped }),
    posthog: live,
    initConfig: () => {
      if (!received) throw new Error(`posthog.init has not run for ${name}`);
      return received;
    },
    captured: named,
    only: (eventName) => {
      const matches = named(eventName);
      const [match] = matches;
      if (!match || matches.length > 1) {
        const count = matches.length;
        throw new Error(`expected exactly one ${eventName}, got ${count}`);
      }
      return match;
    },
    clear: () => {
      captured.length = 0;
    },
    shutdown: () => {
      recording = false;
      client?.reset(true);
    },
  };
}
