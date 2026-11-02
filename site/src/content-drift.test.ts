// @vitest-environment node
// The landing page quotes rr: commands, error codes, messages and recorded output.
// These checks read the repository around the site so a renamed flag, a new
// message or a stale recording fails here instead of shipping.
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { parse } from "yaml";
import { site } from "./config";
import {
  FILTERED,
  FULL,
  firstFailure,
  goFailureLine,
  loadAgentRecording,
} from "./data/agent-session";
import { CAPTIONS, loadCommands } from "./data/commands";
import { HERO_RUN, loadHeroRun } from "./data/hero-run";
import { POLL_MS } from "./lib/flow";
import { WAIT_TIMEOUT, waitMessage } from "./lib/queue";

const repo = join(process.cwd(), "..");
const read = (path: string) => readFileSync(join(repo, path), "utf8");
const readme = read("README.md");
const goCli = readdirSync(join(repo, "internal/cli"))
  .filter((file) => file.endsWith(".go") && !file.endsWith("_test.go"))
  .map((file) => read(`internal/cli/${file}`))
  .join("\n");
const subcommands = new Set([...goCli.matchAll(/Use:\s+"([\w-]+)/g)].map((match) => match[1]));
const repoTasks = Object.keys(
  (parse(read(".rr.yaml")) as { tasks: Record<string, unknown> }).tasks,
);

/** `rr --pretty run "…"` → "run": the subcommand or task a command line calls. */
const rrVerb = (command: string) =>
  command
    .split(/\s+/)
    .slice(1)
    .find((word) => !word.startsWith("-"));

describe("copy matches the README", () => {
  it.each(site.install)("$label install command", ({ command }) => {
    expect(readme).toContain(command);
  });

  it.each(site.plugin)("plugin step %s", (command) => {
    expect(readme).toContain(command);
  });

  it("quotes the README's parallel run and its timings", () => {
    for (const line of site.parallel.output.filter(Boolean)) expect(readme).toContain(line.trim());
    expect(readme).toContain(`about ${site.parallel.sequentialSeconds} seconds`);
    expect(readme).toContain(`${site.parallel.parallelSeconds} seconds`);
  });

  it.each(site.compare)("comparison row %s", (tool) => {
    expect(readme).toContain(tool);
  });

  it.each(site.fallback.rows)("fallback row %s", (where) => {
    expect(readme).toContain(where);
  });
});

describe("copy matches the Go source", () => {
  it.each(site.errorCodes)("error code %s exists", (code) => {
    expect(read("internal/cli/json.go")).toContain(`"${code}"`);
  });

  it("uses rr's wait message and its default one-minute wait", () => {
    const loadbalance = read("internal/cli/loadbalance.go");
    expect(loadbalance).toContain('"All hosts locked by your own runs (%s); waiting up to %s"');
    expect(loadbalance).toContain('desc = fmt.Sprintf("pid %d: %s", h.Pid, h.Command)');
    expect(loadbalance).toContain("waitTimeout = 1 * time.Minute");
    expect(WAIT_TIMEOUT).toBe("1m0s");
    expect(waitMessage({ id: 1, pid: 7, command: "x" })).toContain("(pid 7: x)");
  });

  it("retries the locks as often as the How it works diagram says", () => {
    expect(read("internal/cli/loadbalance.go")).toContain(
      `time.NewTimer(${POLL_MS / 1000} * time.Second)`,
    );
  });
});

describe("recorded fixtures", () => {
  it("the hero run has every captured host go idle, run a suite, and go idle again", () => {
    const { timeline } = loadHeroRun();
    expect(timeline.hosts).toEqual(HERO_RUN.hosts);
    for (const host of timeline.hosts) {
      const statuses = timeline.frames.map((frame) => frame.hosts[host]?.status);
      const firstRun = statuses.indexOf("running");
      expect(statuses[0], host).toBe("idle");
      expect(firstRun, host).toBeGreaterThan(0);
      expect(statuses.at(-1), host).toBe("idle");
      const commands = timeline.frames.map((frame) => frame.hosts[host]?.command).filter(Boolean);
      expect(commands.length, host).toBeGreaterThan(0);
    }
  });

  it("the hero events fall inside the monitor recording and end with a result", () => {
    const { events, timeline } = loadHeroRun();
    expect(events.length).toBeGreaterThan(0);
    for (const event of events) expect(event.atMs).toBeLessThanOrEqual(timeline.durationMs);
    expect(JSON.parse(events.at(-1)?.text ?? "{}").type).toBe("result");
  });

  it("every Commands tab has a recording, a caption and a real subcommand or task", () => {
    const commands = loadCommands();
    expect(commands.map((command) => command.name).sort()).toEqual(Object.keys(CAPTIONS).sort());
    for (const command of commands) {
      expect(command.lines.length, command.name).toBeGreaterThan(0);
      const verb = rrVerb(command.command) ?? "";
      expect(
        subcommands.has(verb) || repoTasks.includes(verb),
        `${verb} is a subcommand or task`,
      ).toBe(true);
    }
  });

  it("the agent session fails on a named test with a file:line, then passes, then runs a green full suite", () => {
    const recording = loadAgentRecording();
    expect(recording.fail.result?.status).not.toBe("success");
    expect(firstFailure(recording.fail)?.name).toBe("TestItoa");
    expect(goFailureLine(recording.fail)?.where).toMatch(/^lock_test\.go:\d+$/);
    expect(recording.pass.result?.status).toBe("success");
    expect(recording.full.result?.status).toBe("success");
    expect(repoTasks).toContain(rrVerb(FULL));
    expect(subcommands.has(rrVerb(FILTERED) ?? "")).toBe(true);
    expect(existsSync(join(process.cwd(), "fixtures/agent/break.patch"))).toBe(true);
  });
});
