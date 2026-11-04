// PostHog remote config for the analytics tests: the real response for the Steamfield
// project (fixtures/posthog/README.md says how to refresh it), and that response with
// every switch the project's settings can turn on, turned on.
import projectConfig from "../fixtures/posthog/remote-config.json" with { type: "json" };

export type RemoteConfig = Record<string, unknown>;

export const project: RemoteConfig = projectConfig;

export const everythingOn: RemoteConfig = {
  ...projectConfig,
  hasFeatureFlags: true,
  autocapture_opt_out: false,
  capturePerformance: {
    network_timing: true,
    web_vitals: true,
    web_vitals_allowed_metrics: null,
  },
  autocaptureExceptions: true,
  errorTracking: { autocaptureExceptions: true, suppressionRules: [] },
  captureDeadClicks: true,
  logs: { captureConsoleLogs: true },
  sessionRecording: {
    endpoint: "/s/",
    consoleLogRecordingEnabled: true,
    recordCanvas: false,
    sampleRate: null,
    minimumDurationMilliseconds: null,
    linkedFlag: null,
    networkPayloadCapture: { recordBody: true, recordHeaders: true },
    masking: { maskAllInputs: false, maskTextSelector: null },
    urlTriggers: [],
    urlBlocklist: [],
    eventTriggers: [],
  },
  surveys: true,
  productTours: true,
  conversations: {
    enabled: true,
    widgetEnabled: true,
    token: "conversations-test",
  },
};
