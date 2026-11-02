// Turns a raw capture (fixtures/raw/, from capture-demo.sh) into the committed
// fixtures the landing page reads, and prints every replacement for review.
//
//   node scripts/scrub-fixtures.mjs <raw-dir> <out-dir>
//
// Kept: what a monitor card shows (cpu, ram, gpu, disk, net, latency, the lock's
// command and start time) and rr's own output. Removed or replaced: the home
// directory, the user name, lock holders, and OS details the card never shows.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { counts, printSummary, scrubText, stripTerminal } from "./scrub-lib.mjs";

const note = (label) => counts.set(label, (counts.get(label) ?? 0) + 1);

const [raw, out] = process.argv.slice(2);
if (!raw || !out) {
  console.error("usage: node scripts/scrub-fixtures.mjs <raw-dir> <out-dir>");
  process.exit(2);
}

function cardHost(host) {
  const keep = {
    name: host.name,
    alias: host.alias,
    online: host.online,
    latency_ms: host.latency_ms,
    cpu: host.cpu,
    ram: host.ram && {
      percent: host.ram.percent,
      used_bytes: host.ram.used_bytes,
      total_bytes: host.ram.total_bytes,
    },
    gpu: host.gpu && { name: host.gpu.name, percent: host.gpu.percent },
    disk: host.disk && { percent: host.disk.percent },
    net: host.net,
  };
  if (host.error) keep.error = scrubText(host.error);
  if (host.lock) {
    note("lock holder dropped");
    keep.lock = { command: scrubText(host.lock.command ?? ""), started: host.lock.started };
  }
  return keep;
}

mkdirSync(out, { recursive: true });

const snapshots = readFileSync(join(raw, "monitor.jsonl"), "utf8")
  .split("\n")
  .filter(Boolean)
  .map((line) => JSON.parse(line))
  .map((snap) => ({ timestamp: snap.timestamp, hosts: snap.hosts.map(cardHost) }));
writeFileSync(
  join(out, "monitor.jsonl"),
  `${snapshots.map((s) => JSON.stringify(s)).join("\n")}\n`,
);

// A failed task's output_tail is the end of its log: test output, request logs and
// file paths from the project. The page shows that a tail exists, not its content.
function trimTails(value) {
  if (Array.isArray(value)) return value.map(trimTails);
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, inner]) => {
        if (key === "output_tail" && typeof inner === "string") {
          note("output_tail trimmed");
          return [key, `… ${inner.split("\n").length} lines`];
        }
        return [key, trimTails(inner)];
      }),
    );
  }
  return value;
}

const events = readFileSync(join(raw, "run.jsonl"), "utf8")
  .split("\n")
  .filter((line) => line.startsWith("{"))
  .map((line) => scrubText(JSON.stringify(trimTails(JSON.parse(line)))));
writeFileSync(join(out, "run.jsonl"), `${events.join("\n")}\n`);

const stdout = join(raw, "run.stdout");
if (existsSync(stdout)) {
  writeFileSync(
    join(out, "run.stdout.txt"),
    scrubText(stripTerminal(readFileSync(stdout, "utf8"))),
  );
}

const pretty = join(raw, "run.pretty.ansi");
if (existsSync(pretty)) {
  writeFileSync(
    join(out, "run.pretty.txt"),
    scrubText(stripTerminal(readFileSync(pretty, "utf8"))),
  );
}

console.log(
  `Wrote ${snapshots.length} monitor snapshots and ${events.length} run events to ${out}.`,
);
printSummary();
