import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderToString } from "react-dom/server";
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
const currentStep = () => document.querySelector('[aria-current="step"]')?.textContent ?? null;
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

  it("server-renders the held frame with every step written out and no controls", () => {
    setMotion(false);
    document.body.innerHTML = renderToString(<FlowDiagram />);
    expect(holders()).toEqual(["0", "0", "0"]);
    expect(screen.getAllByRole("listitem")).toHaveLength(5);
    expect(screen.getByText("Agent 2 and Agent 3 wait")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("advances through the steps while playing and holds still while paused", async () => {
    setMotion(false);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<FlowDiagram />);
    expect(currentStep()).toBe("Agent 1 takes every host");
    await act(() => vi.advanceTimersByTimeAsync(4500));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
    await user.click(screen.getByRole("button", { name: "Play" }));
    await act(() => vi.advanceTimersByTimeAsync(11_000));
    expect(currentStep()).toBe("Agent 4 gets m1-mini");
  });

  it("resumes playing when a step is picked while paused", async () => {
    setMotion(false);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<FlowDiagram />);
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.click(screen.getByRole("button", { name: "Agent 1 takes every host" }));
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(4500));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
  });

  it("starts over after holding the last frame", async () => {
    setMotion(false);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<FlowDiagram />);
    await user.click(screen.getByRole("button", { name: /^Agent 4 gets/ }));
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(currentStep()).toMatch(/^Agent 4 gets/);
    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(currentStep()).toBe("Agent 1 takes every host");
    await act(() => vi.advanceTimersByTimeAsync(4500));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
  });

  it("doesn't advance while off screen", async () => {
    setMotion(false);
    let report: (visible: boolean) => void = () => {};
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(callback: IntersectionObserverCallback) {
          report = (visible) =>
            callback(
              [{ isIntersecting: visible, target: this.target } as IntersectionObserverEntry],
              this as unknown as IntersectionObserver,
            );
        }
        target: Element | undefined;
        observe(el: Element) {
          this.target = el;
        }
        unobserve() {}
        disconnect() {}
      },
    );
    render(<FlowDiagram />);
    act(() => report(true));
    await act(() => vi.advanceTimersByTimeAsync(4500));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
    act(() => report(false));
    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
  });
});
