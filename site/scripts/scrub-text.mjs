// Scrubs one captured terminal file into a committed fixture (see scrub-lib.mjs).
//
//   node scripts/scrub-text.mjs <in> <out> [<in> <out> ...]
import { readFileSync, writeFileSync } from "node:fs";
import { printSummary, scrubText, stripTerminal } from "./scrub-lib.mjs";

const args = process.argv.slice(2);
if (args.length === 0 || args.length % 2 !== 0) {
  console.error("usage: node scripts/scrub-text.mjs <in> <out> [<in> <out> ...]");
  process.exit(2);
}
for (let i = 0; i < args.length; i += 2) {
  writeFileSync(args[i + 1], scrubText(stripTerminal(readFileSync(args[i], "utf8"))));
}
printSummary();
