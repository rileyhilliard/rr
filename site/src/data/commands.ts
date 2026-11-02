// Build-time loader for the Commands section. scripts/capture-commands.sh
// (`make capture-commands`) recorded each command's real --pretty output into
// fixtures/commands/; this adds the one-line captions and tones the lines.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { CommandDemo } from "../components/landing/CommandTabs";

const DIR = join(process.cwd(), "fixtures", "commands");

export const CAPTIONS: Record<string, string> = {
  init: "Once per project: writes .rr.yaml (recorded with its non-interactive flags)",
  doctor: "When something's off: checks SSH, rsync, config and every host",
  status: "Which hosts answer, over which route, and who holds a lock",
  run: "Any command: lock a free host, sync, run, stream back",
  test: "A named task; parallel ones fan out across hosts",
  logs: "Recent runs and where their per-task logs live",
};

type ManifestEntry = { name: string; command: string; exit_code: number };

export function loadCommands(dir = DIR): CommandDemo[] {
  const manifest = JSON.parse(readFileSync(join(dir, "manifest.json"), "utf8")) as ManifestEntry[];
  return manifest.map((entry) => ({
    name: entry.name,
    command: entry.command,
    // Flags that only made the recording non-interactive stay out of the tab label.
    label: entry.command.replace(/ --\S+(?: (?!--)\S+)?/g, "").trim(),
    caption: CAPTIONS[entry.name] ?? "",
    lines: readFileSync(join(dir, `${entry.name}.txt`), "utf8")
      .replace(/\n+$/, "")
      .split("\n"),
  }));
}
