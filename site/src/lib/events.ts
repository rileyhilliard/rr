// The event catalog. track() accepts only these names. Name events object_verb in
// the past tense (install_copied) and keep properties snake_case. No SDK import here,
// so server code and tests can read the catalog without loading posthog-js.
export const Events = {
  /** An install command was copied; `method` is the install tab (Homebrew, Script, Go). */
  installCopied: "install_copied",
  /** A Claude Code plugin command was copied. */
  pluginCopied: "plugin_copied",
  /** A tracked landing section scrolled into view; `id` is the section anchor. */
  sectionViewed: "section_viewed",
  /** A command tab was opened in the Commands section; `name` is the command. */
  commandTabOpened: "command_tab_opened",
} as const;

export type EventName = (typeof Events)[keyof typeof Events];

// Property values are flat scalars: never free text a user typed, emails or tokens.
export type EventProps = Record<string, string | number | boolean | null>;
