# PostHog remote config

`remote-config.json` is the response PostHog's CDN returned for the Steamfield project's public key, `GET https://us-assets.i.posthog.com/array/phc_ydp4qxgAzjHBKiH6oGe8WzCfnPtVc4hwF3NHY3VK3Uca/config`, unedited. The key is the public project key the sites ship; the response holds no secret.

The tests use it in two ways (`tests/helpers/posthog-remote-config.ts`): as returned, and as the base of `everythingOn`, which keeps its shape and turns on every switch the project's settings can turn on. The production branch loads the same object as a script, `/array/<key>/config.js`, which sets `window._POSTHOG_REMOTE_CONFIG[key] = { config, siteApps }`; the test harness and the Chromium spec (`e2e/analytics.spec.ts`) serve it that way.

Refresh it when posthog-js is upgraded, or when PostHog's response gains fields:

```sh
curl -sS https://us-assets.i.posthog.com/array/phc_ydp4qxgAzjHBKiH6oGe8WzCfnPtVc4hwF3NHY3VK3Uca/config \
  | python3 -m json.tool --indent 2 > tests/fixtures/posthog/remote-config.json
```

Then run `bunx vitest run src/lib/analytics.test.ts` and `bunx playwright test e2e/analytics.spec.ts`. A new switch the project can turn on belongs in `everythingOn` too.
