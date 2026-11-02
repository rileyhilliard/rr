import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { CommandTabs } from "./CommandTabs";

const commands = [
  {
    name: "doctor",
    command: "rr doctor",
    caption: "Checks hosts",
    lines: ["All checks passed"],
  },
  {
    name: "status",
    command: "rr status",
    caption: "Who holds a lock",
    lines: ["m4-mini connected"],
  },
];

describe("CommandTabs", () => {
  it("switches commands by click and arrow keys, and records the tab in the URL", async () => {
    const user = userEvent.setup();
    render(<CommandTabs commands={commands} />);
    expect(screen.getByRole("tab", { name: /rr doctor/ })).toHaveAttribute("aria-selected", "true");
    await user.click(screen.getByRole("tab", { name: /rr status/ }));
    expect(screen.getByRole("tabpanel")).toHaveAttribute("id", "cmd-status");
    expect(window.location.hash).toBe("#cmd-status");
    await user.keyboard("{ArrowDown}");
    expect(screen.getByRole("tab", { name: /rr doctor/ })).toHaveFocus();
    expect(screen.getByRole("tabpanel")).toHaveAttribute("id", "cmd-doctor");
  });

  it("announces tab switches for analytics", async () => {
    const user = userEvent.setup();
    const seen: string[] = [];
    const listener = (event: Event) =>
      seen.push((event as CustomEvent<{ name: string }>).detail.name);
    window.addEventListener("rr:command-tab", listener);
    render(<CommandTabs commands={commands} />);
    await user.click(screen.getByRole("tab", { name: /rr status/ }));
    window.removeEventListener("rr:command-tab", listener);
    expect(seen).toEqual(["status"]);
  });

  it("renders nothing without commands", () => {
    const { container } = render(<CommandTabs commands={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});
