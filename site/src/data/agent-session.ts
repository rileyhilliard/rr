// Build-time loader for the Agent session. The rr output in every tool call is a
// real recording (scripts/capture-agent.sh, `make capture-agent`): a filtered run
// that fails, the same run after the fix, then the full suite. The user's request
// and the agent's sentences are written here, and the page says so.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { Line, Step } from "../components/landing/TerminalPlayer";

const DIR = join(process.cwd(), "fixtures", "agent");
export const FILTERED = 'rr run "go test ./internal/lock/... -run TestItoa"';
export const FULL = "rr test";
/** Tool output lines shown before the rest collapse into "… +N lines". */
const SHOWN = 6;

type Recording = { events: Line[]; stdout: string[]; result: Record<string, unknown> | undefined };

function read(dir: string, name: string): Recording {
  const eventLines = readFileSync(join(dir, `${name}.jsonl`), "utf8")
    .split("\n")
    .filter((line) => line.startsWith("{"));
  const parsed = eventLines.map((line) => JSON.parse(line) as Record<string, unknown>);
  const result = parsed.find((event) => event.type === "result");
  return {
    events: eventLines.map((text, i) => ({
      text,
      tone:
        parsed[i]?.type === "result"
          ? parsed[i]?.status === "success"
            ? "ok"
            : "fail"
          : ("muted" as const),
    })),
    stdout: readFileSync(join(dir, `${name}.stdout.txt`), "utf8")
      .replace(/\n+$/, "")
      .split("\n")
      .filter(Boolean),
    result,
  };
}

/** What a coding agent's tool view shows: stdout, then rr's events, the tail collapsed. */
function toolOutput(recording: Recording): Step {
  const all: Line[] = [...recording.stdout.map((text) => ({ text })), ...recording.events];
  const resultLine = recording.events.at(-1);
  const head = all.slice(0, Math.max(0, SHOWN - 1)).filter((line) => line !== resultLine);
  const more = all.length - head.length - (resultLine ? 1 : 0);
  return {
    kind: "tool-output",
    lines: resultLine
      ? [
          ...head,
          ...(more > 0 ? [{ text: `… +${more} lines`, tone: "muted" as const }] : []),
          resultLine,
        ]
      : head,
  };
}

export type AgentRecording = { fail: Recording; pass: Recording; full: Recording };

export function loadAgentRecording(dir = DIR): AgentRecording {
  return { fail: read(dir, "fail"), pass: read(dir, "pass"), full: read(dir, "full") };
}

type Failure = { file?: string; line?: number; location?: string; message?: string; name?: string };

export function firstFailure(recording: Recording): Failure | undefined {
  const details = recording.result?.details as { failures?: Failure[] } | undefined;
  return details?.failures?.[0];
}

export function summaryOf(recording: Recording): string {
  const details = recording.result?.details as { summary?: Record<string, number> } | undefined;
  const summary = details?.summary;
  if (!summary) return "";
  return Object.entries(summary)
    .filter(([, value]) => typeof value === "number" && value > 0)
    .map(([key, value]) => `${value} ${key}`)
    .join(", ");
}

/** The first `file_test.go:LINE: message` line Go's test output printed. */
export function goFailureLine(
  recording: Recording,
): { where: string; message: string } | undefined {
  for (const line of recording.stdout) {
    const match = /^\s*(\w+_test\.go:\d+): (.+)$/.exec(line);
    if (match?.[1] && match[2]) return { where: match[1], message: match[2] };
  }
  return undefined;
}

export function agentSteps(recording: AgentRecording): Step[] {
  const failure = firstFailure(recording.fail);
  const output = goFailureLine(recording.fail);
  const fullSummary = summaryOf(recording.full);
  return [
    {
      kind: "user",
      text: "TestItoa started failing after the refactor in internal/lock. Fix it and make sure nothing else broke.",
    },
    {
      kind: "agent",
      text: "I'll run just that test on a remote host first, so the full suite doesn't tie up a machine.",
    },
    { kind: "tool", name: "Bash", arg: FILTERED },
    toolOutput(recording.fail),
    {
      kind: "agent",
      text: `The result event says ${failure?.name ?? "the test"} failed, and the test output has the line: ${output?.where ?? "lock_test.go"}, ${output?.message ?? "itoa returns an empty string for negative numbers"}. The refactor dropped itoa's negative-number branch. Restoring it.`,
    },
    { kind: "tool", name: "Update", arg: "internal/lock/info.go" },
    {
      kind: "tool-output",
      lines: [
        { text: "Added 3 lines" },
        { text: "+	if n < 0 {", tone: "ok" },
        { text: '+		return "-" + itoa(-n)', tone: "ok" },
        { text: "+	}", tone: "ok" },
      ],
    },
    { kind: "tool", name: "Bash", arg: FILTERED },
    toolOutput(recording.pass),
    { kind: "agent", text: "That passes. Now the whole suite, as the gate before I call it done." },
    { kind: "tool", name: "Bash", arg: FULL },
    toolOutput(recording.full),
    {
      kind: "agent",
      text: `Fixed: itoa handles negative numbers again (internal/lock/info.go). TestItoa passes on its own, and the full suite is green${fullSummary ? ` (${fullSummary})` : ""}.`,
    },
  ];
}
