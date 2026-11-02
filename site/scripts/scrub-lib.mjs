// Shared by the capture scrubbers: what gets replaced before a recording is
// committed, and how terminal output becomes plain text.
import { hostname, userInfo } from "node:os";

const user = userInfo().username;
// laptop.lan → laptop: the name other machines record for this one.
const machine = hostname().replace(/\.(local|lan)$/i, "");

const escapeRegExp = (value) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/** Counts every replacement by label, for the review summary. */
export const counts = new Map();
const note = (label) => counts.set(label, (counts.get(label) ?? 0) + 1);

export function scrubText(text) {
  let result = text;
  for (const [pattern, replacement, label] of [
    // Any macOS or Linux home, on a path boundary: other machines' homes show up too
    // (a remote's .rr-source names the checkout it came from).
    [/\/(?:Users|home)\/[^/\s"']+(?=\/|\b)/g, "~", "home directory"],
    [
      new RegExp(`\\b${escapeRegExp(machine)}(\\.local|\\.lan)?\\b`, "gi"),
      "laptop",
      "machine name",
    ],
    [new RegExp(`\\b${escapeRegExp(user)}s?\\b`, "gi"), "you", "user name"],
  ]) {
    result = result.replace(pattern, (match) => {
      note(`${label} (${match})`);
      return replacement;
    });
  }
  return result;
}

/** ANSI codes out; for lines a spinner rewrote with \r, what was left on screen. */
export function stripTerminal(text) {
  return (
    text
      // biome-ignore lint/suspicious/noControlCharactersInRegex: matching ESC is the point (ANSI CSI sequences)
      .replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, "")
      // biome-ignore lint/suspicious/noControlCharactersInRegex: matching ESC and BEL is the point (OSC sequences)
      .replace(/\x1b\][^\x07]*\x07/g, "")
      .split("\n")
      .map((line) => line.split("\r").filter(Boolean).at(-1) ?? "")
      .join("\n")
  );
}

export function printSummary() {
  console.log("Replacements (review these before committing):");
  for (const [label, count] of counts) console.log(`  ${count}x ${label}`);
  if (counts.size === 0) console.log("  none");
}
