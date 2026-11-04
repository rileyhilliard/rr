import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { hydrateRoot } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  setTabHidden,
  stubIntersectionObserver,
  stubReducedMotion,
} from "../../../tests/helpers/dom";
import { FlowDiagram } from "./FlowDiagram";

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
    setTabHidden(false);
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("holds the parallel run on every host with two runs waiting, and lists every step", () => {
    stubReducedMotion(true);
    render(<FlowDiagram />);
    expect(holders()).toEqual(["0", "0", "0"]);
    expect(chipState(1)).toBe("waiting");
    expect(chipState(2)).toBe("waiting");
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull();
    expect(screen.getAllByRole("listitem")).toHaveLength(5);
    expect(screen.getAllByRole("img")[0]).toHaveAccessibleName(/no server in between/);
  });

  it("plays from the start and jumps to a step when it's picked", async () => {
    stubReducedMotion(false);
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
    stubReducedMotion(false);
    document.body.innerHTML = renderToString(<FlowDiagram />);
    expect(holders()).toEqual(["0", "0", "0"]);
    expect(screen.getAllByRole("listitem")).toHaveLength(5);
    expect(screen.getByText("Agent 2 and Agent 3 wait")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("advances through the steps while playing and holds still while paused", async () => {
    stubReducedMotion(false);
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
    stubReducedMotion(false);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<FlowDiagram />);
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.click(screen.getByRole("button", { name: "Agent 1 takes every host" }));
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(4500));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
  });

  it("starts over after holding the last frame", async () => {
    stubReducedMotion(false);
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

  it("stops while off screen and picks up where it left off when back", async () => {
    stubReducedMotion(false);
    const { intersect } = stubIntersectionObserver();
    const { container } = render(<FlowDiagram />);
    const root = container.firstElementChild as Element;
    act(() => intersect(root));
    await act(() => vi.advanceTimersByTimeAsync(4500));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
    act(() => intersect(root, false));
    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
    act(() => intersect(root));
    await act(() => vi.advanceTimersByTimeAsync(6000));
    expect(currentStep()).toBe("Agent 2 gets m4-mini");
  });

  it("stops while the tab is hidden and resumes when it's shown", async () => {
    stubReducedMotion(false);
    render(<FlowDiagram />);
    await act(() => vi.advanceTimersByTimeAsync(4500));
    act(() => setTabHidden(true));
    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
    act(() => setTabHidden(false));
    await act(() => vi.advanceTimersByTimeAsync(6000));
    expect(currentStep()).toBe("Agent 2 gets m4-mini");
  });

  it("shows the held frame while motion is reduced, then resumes at the same step", async () => {
    const motion = stubReducedMotion(false);
    render(<FlowDiagram />);
    await act(() => vi.advanceTimersByTimeAsync(4500));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
    act(() => motion.set(true));
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull();
    expect(holders()).toEqual(["0", "0", "0"]);
    await act(() => vi.advanceTimersByTimeAsync(10_000));
    act(() => motion.set(false));
    expect(currentStep()).toBe("Agent 2 and Agent 3 wait");
  });

  it("hydrates the server HTML without a mismatch and then shows its controls", async () => {
    stubReducedMotion(false);
    const container = document.createElement("div");
    container.innerHTML = renderToString(<FlowDiagram />);
    document.body.append(container);
    const onRecoverableError = vi.fn();
    const root = await act(async () =>
      hydrateRoot(container, <FlowDiagram />, { onRecoverableError }),
    );
    expect(onRecoverableError).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    act(() => root.unmount());
    container.remove();
  });
});
