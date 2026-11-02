import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { buildTimeline, type Snapshot } from "../../lib/monitor/timeline";
import { MonitorStage, type StageProps } from "./MonitorStage";

const sample = (name: string, cpu: number, lock?: string) => ({
  name,
  alias: `${name}-local`,
  online: true,
  latency_ms: 20,
  cpu: { percent: cpu, cores: 8, load: [2, 1, 1] },
  ram: { percent: 40, used_bytes: 1, total_bytes: 2 },
  ...(lock ? { lock: { command: lock, started: "2026-11-02T16:41:00Z" } } : {}),
});

const snapshots: Snapshot[] = [
  { timestamp: "2026-11-02T16:40:58Z", hosts: [sample("mbp", 5), sample("m4-mini", 3)] },
  {
    timestamp: "2026-11-02T16:41:20Z",
    hosts: [sample("mbp", 92, "uv run pytest tests/api"), sample("m4-mini", 70, "bun test")],
  },
  { timestamp: "2026-11-02T16:41:40Z", hosts: [sample("mbp", 8), sample("m4-mini", 4)] },
];

const props: StageProps = {
  timeline: buildTimeline(snapshots, ["mbp", "m4-mini"]),
  events: [
    { atMs: 3000, text: '{"type":"phase","phase":"connect","status":"complete"}' },
    { atMs: 40000, text: '{"type":"result","status":"success"}', tone: "ok" },
  ],
  pretty: [
    { atMs: 0, text: "Parallel Execution Summary" },
    { atMs: 0, text: "  ◉ test-api on mbp (40.0s)", tone: "ok" },
  ],
  task: "test",
  project: "OpenData",
  speed: 1,
};

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

const grid = () => document.querySelector(".lg\\:grid-cols-3") as HTMLElement;

describe("MonitorStage", () => {
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

  it("holds the busiest frame and the whole transcript under reduced motion", () => {
    setMotion(true);
    render(<MonitorStage {...props} />);
    const cards = within(grid()).getAllByRole("article");
    expect(cards.map((card) => card.dataset.status)).toEqual(["running", "running"]);
    expect(within(grid()).getByText(/> uv run pytest tests\/api/)).toBeInTheDocument();
    expect(cards[0]).toHaveAttribute("data-firing");
    expect(screen.getByRole("region", { name: "Pretty output" })).toHaveTextContent(
      "test-api on mbp",
    );
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull();
  });

  it("swaps between the human and agent transcripts", async () => {
    setMotion(true);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<MonitorStage {...props} />);
    await user.click(screen.getByRole("button", { name: "Agent" }));
    const agent = screen.getByRole("region", { name: "JSON events" });
    expect(agent).toHaveTextContent('"type":"result"');
    expect(agent.className).toContain("h-56");
  });

  it("replays from idle through running and back", async () => {
    setMotion(false);
    render(<MonitorStage {...props} />);
    await act(() => vi.advanceTimersByTimeAsync(200));
    const status = () => within(grid()).getAllByRole("article")[0]?.dataset.status;
    expect(status()).toBe("idle");
    await act(() => vi.advanceTimersByTimeAsync(22_000));
    expect(status()).toBe("running");
    await act(() => vi.advanceTimersByTimeAsync(20_000));
    expect(status()).toBe("idle");
    expect(screen.getAllByRole("button", { name: "Replay" }).length).toBeGreaterThan(0);
  });

  it("switches the phone view's host with arrow keys", async () => {
    setMotion(true);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<MonitorStage {...props} />);
    const tabs = screen.getByRole("tablist", { name: "Hosts" });
    await user.click(within(tabs).getByRole("tab", { name: /mbp/ }));
    await user.keyboard("{ArrowRight}");
    const selected = within(tabs).getByRole("tab", { name: /m4-mini/ });
    expect(selected).toHaveAttribute("aria-selected", "true");
    // Phones show only the selected host's card; the others are hidden below sm.
    const panel = document.getElementById(selected.getAttribute("aria-controls") ?? "");
    expect(panel).toHaveTextContent("m4-mini");
    expect(panel?.className).not.toContain("hidden");
    const mbpPanel = document.getElementById(
      within(tabs).getByRole("tab", { name: /mbp/ }).getAttribute("aria-controls") ?? "",
    );
    expect(mbpPanel?.className).toContain("hidden");
  });
});
