import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FlowDiagram } from "./FlowDiagram";

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

const wide = () => document.querySelectorAll("svg")[0] as SVGElement;
const chipState = (i: number) =>
  wide().querySelector(`[data-chip="${i}"]`)?.getAttribute("data-state") ?? null;
const holders = () =>
  [...wide().querySelectorAll("[data-host]")].map((host) => host.getAttribute("data-holder"));

describe("FlowDiagram", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.stubGlobal("IntersectionObserver", undefined);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("holds the parallel run on every host with two runs waiting, and lists every step", () => {
    setMotion(true);
    render(<FlowDiagram />);
    expect(holders()).toEqual(["0", "0", "0"]);
    expect(chipState(1)).toBe("waiting");
    expect(chipState(2)).toBe("waiting");
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull();
    expect(screen.getAllByRole("listitem")).toHaveLength(5);
    expect(screen.getAllByRole("img")[0]).toHaveAccessibleName(/no server in between/);
  });

  it("plays from the start and jumps to a step when it's picked", async () => {
    setMotion(false);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<FlowDiagram />);
    expect(chipState(0)).toBeNull();
    await user.click(screen.getByRole("button", { name: "Agent 3 gets m1-linux" }));
    await act(() => vi.advanceTimersByTimeAsync(1500));
    expect(holders().slice(0, 2)).toEqual(["1", "2"]);
    expect(screen.getByRole("button", { name: "Agent 3 gets m1-linux" })).toHaveAttribute(
      "aria-current",
      "step",
    );
    expect(screen.getByText(/moves down the list to m1-linux/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Pause" }));
    expect(screen.getByRole("button", { name: "Play" })).toBeInTheDocument();
  });
});
