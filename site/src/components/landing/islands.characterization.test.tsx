// Characterization of the three islands that share the terminal and tabs code: what the
// server sends (the no-JS page), replay, and the props call sites use.
import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderToString } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CommandTabs } from "./CommandTabs";
import { InstallTabs } from "./InstallTabs";
import { type Step, schedule, TerminalPlayer } from "./TerminalPlayer";

const track = vi.fn();
vi.mock("../../lib/analytics", () => ({ track: (...args: unknown[]) => track(...args) }));

const steps: Step[] = [
  { kind: "command", text: "rr doctor" },
  {
    kind: "output",
    lines: [
      { text: "All checks passed", tone: "ok" },
      { text: "1 failed", tone: "fail" },
    ],
  },
  { kind: "user", text: "Fix it" },
  { kind: "agent", text: "On it." },
  { kind: "tool", name: "Bash", arg: "rr test" },
  { kind: "tool-output", lines: [{ text: "ok" }], more: 2 },
  { kind: "note", text: "(illustrative)" },
];

const commands = [
  { name: "doctor", command: "rr doctor", caption: "Checks hosts", lines: ["All checks passed"] },
  { name: "status", command: "rr status", caption: "Locks", lines: ["m4-mini connected"] },
];

const install = [
  { label: "Homebrew", command: "brew install rr" },
  { label: "Go", command: "go install rr@latest" },
];

function html(markup: string) {
  const container = document.createElement("div");
  container.innerHTML = markup;
  document.body.append(container);
  return container;
}

beforeEach(() => {
  track.mockClear();
  vi.stubGlobal("IntersectionObserver", undefined);
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
  for (const node of document.body.querySelectorAll(":scope > div:not([id])")) node.remove();
});

describe("TerminalPlayer", () => {
  it("server-renders the whole transcript, with prompts, glyphs and tones, and no controls", () => {
    const root = html(renderToString(<TerminalPlayer title="demo" steps={steps} label="Demo" />));
    const region = within(root).getByRole("region", { name: "Demo" });
    expect(region.textContent).toBe(
      [
        "$ rr doctor",
        "All checks passed",
        "1 failed",
        "❯ Fix it",
        "● On it.",
        "● Bash(rr test)",
        "  ⎿  ok",
        "     … +2 lines",
        "(illustrative)",
      ].join(""),
    );
    expect(within(root).getByText("demo")).toBeInTheDocument();
    expect(within(root).queryByRole("button")).toBeNull();
    expect(within(root).getByText("All checks passed")).toHaveClass("text-success");
    expect(within(root).getByText("1 failed")).toHaveClass("text-error");
    expect(region).toHaveAttribute("tabindex", "0");
    expect(region).toHaveClass("h-80");
  });

  it("takes a height class for the transcript", () => {
    const root = html(
      renderToString(
        <TerminalPlayer title="t" steps={steps} label="Demo" heightClass="h-[32rem]" />,
      ),
    );
    expect(within(root).getByRole("region", { name: "Demo" })).toHaveClass("h-[32rem]");
  });

  it("replays from the start after finishing", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<TerminalPlayer title="demo" steps={steps} label="Demo" />);
    const region = screen.getByRole("region", { name: "Demo" });
    await act(() => vi.advanceTimersByTimeAsync(schedule(steps).total + 200));
    expect(region).toHaveTextContent("(illustrative)");
    await user.click(screen.getByRole("button", { name: "Replay" }));
    expect(region).not.toHaveTextContent("(illustrative)");
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
  });
});

describe("CommandTabs", () => {
  it("server-renders every panel as a deep link target, with no tab selected", () => {
    const root = html(renderToString(<CommandTabs commands={commands} />));
    const tabs = within(root).getAllByRole("tab");
    expect(tabs.map((tab) => tab.getAttribute("href"))).toEqual(["#cmd-doctor", "#cmd-status"]);
    for (const tab of tabs) expect(tab).not.toHaveAttribute("aria-selected");
    expect(root.querySelector("#cmd-doctor")).toHaveTextContent("All checks passed");
    expect(root.querySelector("#cmd-status")).toHaveTextContent("m4-mini connected");
    expect(root.querySelector("[hidden]")).toBeNull();
    expect(within(root).getByRole("tablist", { name: "Commands" })).toHaveAttribute(
      "aria-orientation",
      "vertical",
    );
  });

  it("opens the tab the URL names, without reporting it as a switch", async () => {
    window.history.replaceState(null, "", "/#cmd-status");
    const seen: string[] = [];
    const listener = (event: Event) =>
      seen.push((event as CustomEvent<{ name: string }>).detail.name);
    window.addEventListener("rr:command-tab", listener);
    render(<CommandTabs commands={commands} />);
    await act(async () => {});
    window.removeEventListener("rr:command-tab", listener);
    expect(screen.getByRole("tab", { name: /rr status/ })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tabpanel")).toHaveAttribute("id", "cmd-status");
    expect(seen).toEqual([]);
  });

  it("plays the selected command's output with its tones", () => {
    render(
      <CommandTabs
        commands={[{ name: "doctor", command: "rr doctor", caption: "", lines: ["✓ ok"] }]}
      />,
    );
    const panel = screen.getByRole("tabpanel");
    expect(within(panel).getByRole("region", { name: "Output of rr doctor" })).toBeInTheDocument();
  });
});

describe("InstallTabs", () => {
  it("server-renders the first command, with the copy control out of the no-JS page", () => {
    const root = html(renderToString(<InstallTabs options={install} />));
    expect(within(root).getByRole("tablist", { name: "Install with" })).toBeInTheDocument();
    expect(root).toHaveTextContent("brew install rr");
    expect(root).toHaveTextContent("go install rr@latest");
    const visibleButtons = [...root.querySelectorAll("button")].filter(
      (button) => !button.hidden && button.getAttribute("role") !== "tab",
    );
    expect(visibleButtons.map((button) => button.textContent)).not.toContain("Copied");
  });

  it("tracks a successful copy with the tab's label", async () => {
    const user = userEvent.setup();
    render(<InstallTabs options={install} />);
    await user.click(screen.getByRole("tab", { name: "Go" }));
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(await navigator.clipboard.readText()).toBe("go install rr@latest");
    expect(track).toHaveBeenCalledWith("install_copied", { method: "Go" });
  });

  it("resets the copied label when the tab changes", async () => {
    const user = userEvent.setup();
    render(<InstallTabs options={install} />);
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Go" }));
    expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument();
  });
});
