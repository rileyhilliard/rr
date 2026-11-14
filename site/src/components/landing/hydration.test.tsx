// Every island must hydrate without a mismatch: render to HTML as Astro does,
// then hydrate over it and fail on any recoverable error React reports.
import { act } from "@testing-library/react";
import { createElement, type FunctionComponent } from "react";
import { hydrateRoot } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { agentSteps, loadAgentRecording } from "../../data/agent-session";
import { loadCommands } from "../../data/commands";
import { loadHeroRun } from "../../data/hero-run";
import { CommandTabs } from "./CommandTabs";
import { MonitorStage } from "./MonitorStage";
import { QueueBoard } from "./QueueBoard";
import { TerminalPlayer } from "./TerminalPlayer";

async function hydrationErrors<P extends object>(component: FunctionComponent<P>, props: P) {
  const html = renderToString(createElement(component, props));
  const container = document.createElement("div");
  container.innerHTML = html;
  document.body.append(container);
  const errors: string[] = [];
  await act(async () => {
    hydrateRoot(container, createElement(component, props), {
      onRecoverableError: (error) => errors.push(String(error)),
    });
  });
  container.remove();
  return errors;
}

describe("islands hydrate the server HTML without a mismatch", () => {
  afterEach(() => vi.restoreAllMocks());

  it("MonitorStage", async () => {
    expect(await hydrationErrors(MonitorStage, loadHeroRun())).toEqual([]);
  });

  it("CommandTabs", async () => {
    expect(await hydrationErrors(CommandTabs, { commands: loadCommands() })).toEqual([]);
  });

  it("QueueBoard", async () => {
    expect(
      await hydrationErrors(QueueBoard, { hosts: ["m4-mini", "m1-linux", "m1-mini"] }),
    ).toEqual([]);
  });

  it("TerminalPlayer", async () => {
    const props = { title: "t", steps: agentSteps(loadAgentRecording()), label: "Agent" };
    expect(await hydrationErrors(TerminalPlayer, props)).toEqual([]);
  });
});
