import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { QueueBoard } from "./QueueBoard";

const hosts = ["m4-mini", "m1-linux", "m1-mini"];

describe("QueueBoard", () => {
  it("starts with every host locked and one run waiting, with rr's message", () => {
    render(<QueueBoard hosts={hosts} />);
    const rows = within(screen.getByRole("table")).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(3);
    for (const row of rows) expect(row).toHaveTextContent(/agent \d/);
    expect(
      screen.getByText(/^⣾ All hosts locked by your own runs \(pid \d+: .+\); waiting up to 1m0s/),
    ).toBeInTheDocument();
  });

  it("hands a freed host to the waiting run, then frees it when nobody waits", async () => {
    const user = userEvent.setup();
    render(<QueueBoard hosts={hosts} />);
    await user.click(screen.getByRole("button", { name: "Finish run on m1-linux" }));
    expect(screen.getByRole("row", { name: /m1-linux/ })).toHaveTextContent("agent 4");
    expect(screen.getByText(/Nobody waiting/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Finish run on m1-linux" }));
    expect(screen.getByRole("row", { name: /m1-linux/ })).toHaveTextContent("free");
  });

  it("queues another agent's run when every host is busy", async () => {
    const user = userEvent.setup();
    render(<QueueBoard hosts={hosts} />);
    await user.click(screen.getByRole("button", { name: "Start another agent's run" }));
    expect(screen.getByText(/agent 4, agent 5/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start another agent's run" })).toBeDisabled();
  });

  it("renders nothing without hosts", () => {
    const { container } = render(<QueueBoard hosts={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});
