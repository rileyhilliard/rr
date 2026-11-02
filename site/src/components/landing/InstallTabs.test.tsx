import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { InstallTabs } from "./InstallTabs";

const options = [
  { label: "npm", command: "npm install -g acme" },
  { label: "bun", command: "bun add -g acme" },
];

describe("InstallTabs", () => {
  it("shows the first command before any interaction", () => {
    render(<InstallTabs options={options} />);
    expect(screen.getByRole("tab", { name: "npm" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tabpanel")).toHaveTextContent("npm install -g acme");
  });

  it("switches tabs by click and arrow keys", async () => {
    const user = userEvent.setup();
    render(<InstallTabs options={options} />);
    await user.click(screen.getByRole("tab", { name: "bun" }));
    expect(screen.getByRole("tabpanel")).toHaveTextContent("bun add -g acme");
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "npm" })).toHaveFocus();
    expect(screen.getByRole("tabpanel")).toHaveTextContent("npm install -g acme");
  });

  it("copies the visible command", async () => {
    const user = userEvent.setup();
    render(<InstallTabs options={options} />);
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(await navigator.clipboard.readText()).toBe("npm install -g acme");
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
  });

  it("renders nothing without options", () => {
    const { container } = render(<InstallTabs options={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});
