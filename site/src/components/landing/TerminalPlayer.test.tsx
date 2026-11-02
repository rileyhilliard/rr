import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type Step, schedule, TerminalPlayer } from "./TerminalPlayer";

const steps: Step[] = [
  { kind: "user", text: "Fix the failing test" },
  { kind: "tool", name: "Bash", arg: 'rr run "go test ./..."' },
  {
    kind: "tool-output",
    lines: [{ text: '{"type":"result","status":"success"}', tone: "ok" }],
    more: 3,
  },
  { kind: "agent", text: "All green." },
];

function setMotion(reduce: boolean) {
  vi.spyOn(window, "matchMedia").mockImplementation(
    (query: string) =>
      ({
        matches: reduce && query.includes("reduce"),
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
      }) as unknown as MediaQueryList,
  );
}

describe("TerminalPlayer", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    // happy-dom never reports intersection; without the API the demo counts as on screen.
    vi.stubGlobal("IntersectionObserver", undefined);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("schedules steps in order", () => {
    const { starts, total } = schedule(steps);
    expect(starts).toEqual([...starts].sort((a, b) => a - b));
    expect(total).toBeGreaterThan(starts.at(-1) ?? 0);
  });

  it("shows the whole transcript under reduced motion", () => {
    setMotion(true);
    render(<TerminalPlayer title="demo" steps={steps} label="Demo" />);
    const region = screen.getByRole("region", { name: "Demo" });
    expect(region).toHaveTextContent("Fix the failing test");
    expect(region).toHaveTextContent('Bash(rr run "go test ./...")');
    expect(region).toHaveTextContent("… +3 lines");
    expect(region).toHaveTextContent("All green.");
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("plays steps in order, and pausing holds the position", async () => {
    setMotion(false);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<TerminalPlayer title="demo" steps={steps} label="Demo" />);
    const region = screen.getByRole("region", { name: "Demo" });
    expect(region).not.toHaveTextContent("All green.");
    await act(() => vi.advanceTimersByTimeAsync(2500));
    expect(region).toHaveTextContent("Fix the failing test");
    await user.click(screen.getByRole("button", { name: "Pause" }));
    const held = region.textContent;
    await act(() => vi.advanceTimersByTimeAsync(5000));
    expect(region.textContent).toBe(held);
    await user.click(screen.getByRole("button", { name: "Play" }));
    await act(() => vi.advanceTimersByTimeAsync(schedule(steps).total));
    expect(region).toHaveTextContent("All green.");
    expect(screen.getByRole("button", { name: "Replay" })).toBeInTheDocument();
  });
});
