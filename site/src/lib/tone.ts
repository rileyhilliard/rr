// How a line of rr's --pretty output is colored in the Commands terminals:
// failures red, passes green, warnings amber, rules muted. Shared by the build-time
// loader's tests and the CommandTabs island, which colors lines on the client so
// the island's props stay plain strings.
import type { Line } from "../components/landing/TerminalPlayer";

export function toneOf(line: string): Line["tone"] {
  if (/✕|\bFAIL\b|\bfailed\b(?!:? *0)/i.test(line) && !/0 failed/.test(line)) return "fail";
  if (/^\s*[◉✓●]/.test(line) || /\bpassed\b|All checks passed/.test(line)) return "ok";
  if (/⚠|\bwarn(ing)?\b/i.test(line)) return "warn";
  if (/^\s*[─━-]{3,}\s*$/.test(line)) return "muted";
  return undefined;
}
