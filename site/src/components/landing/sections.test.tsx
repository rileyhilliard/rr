import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  BeforeAfter,
  CommandList,
  DataTable,
  Hero,
  ReleaseBanner,
  Requirements,
  Section,
  TaskChart,
  Transcript,
} from "./sections";

describe("landing sections", () => {
  it("links the announcement and renders nothing without one", () => {
    const { container, rerender } = render(
      <ReleaseBanner announcement={{ label: "v1 is out", href: "/releases" }} />,
    );
    expect(screen.getByRole("link", { name: "v1 is out" })).toHaveAttribute("href", "/releases");
    rerender(<ReleaseBanner announcement={null} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("puts the tagline in the page heading", () => {
    render(<Hero tagline="Run tests elsewhere" description="Over SSH." />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Run tests elsewhere");
  });

  it("labels a section by its heading and marks tracked ones", () => {
    render(
      <Section id="queue" title="Two agents at once" lede="They queue." track>
        <p>body</p>
      </Section>,
    );
    const section = screen.getByRole("region", { name: "Two agents at once" });
    expect(section).toHaveAttribute("id", "queue");
    expect(section).toHaveAttribute("data-track-view");
    expect(section).toHaveTextContent("They queue.");
  });

  it("reveals a section as one sequence: heading, then lede, then content", () => {
    render(
      <Section id="queue" title="Two agents at once" lede="They queue.">
        <p>body</p>
      </Section>,
    );
    const section = screen.getByRole("region", { name: "Two agents at once" });
    expect(section).toHaveAttribute("data-reveal");
    expect(screen.getByRole("heading", { name: "Two agents at once" })).toHaveAttribute(
      "data-motion",
      "lift-lg",
    );
    const lede = screen.getByText("They queue.").closest("[data-motion]") as HTMLElement;
    const content = screen.getByText("body").parentElement as HTMLElement;
    expect(lede.style.getPropertyValue("--motion-delay")).toBe("120ms");
    expect(content.style.getPropertyValue("--motion-delay")).toBe("240ms");
  });

  it("sets the laptop beside rr, and renders nothing without lines", () => {
    const { container, rerender } = render(
      <BeforeAfter
        before={{ title: "On the laptop", lines: ["Runs collide"] }}
        after={{ title: "With rr", lines: ["Each run gets a host", "Agents queue"] }}
      />,
    );
    expect(screen.getByRole("heading", { name: /On the laptop/ })).toBeInTheDocument();
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
    rerender(<BeforeAfter before={{ title: "a", lines: [] }} after={{ title: "b", lines: [] }} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders a transcript as text with the prompt hidden from screen readers", () => {
    render(
      <Transcript command="rr --pretty test" lines={["  ◉ test-backend on m1-linux (52.9s)"]} />,
    );
    expect(
      screen.getByText("rr --pretty test").querySelector("[aria-hidden=true]"),
    ).toHaveTextContent("$");
    expect(screen.getByText(/test-backend on m1-linux/)).toBeInTheDocument();
  });

  it("charts the tasks end to end and side by side, and says so in words", () => {
    render(
      <TaskChart
        tasks={[
          { name: "test-backend", host: "m1-linux", seconds: 52.9 },
          { name: "test-frontend", host: "m1-mini", seconds: 4 },
        ]}
      />,
    );
    expect(screen.getByRole("figure")).toHaveAccessibleName(/56\.9 seconds.*52\.9 seconds/);
  });

  it("renders tables with row headers and nothing when empty", () => {
    const { container, rerender } = render(
      <DataTable caption="Tools" head={["Tool", "Gap"]} rows={[["Ansible", "Playbooks"]]} />,
    );
    const table = screen.getByRole("table", { name: "Tools" });
    expect(within(table).getByRole("rowheader", { name: "Ansible" })).toBeInTheDocument();
    rerender(<DataTable caption="Tools" head={["Tool", "Gap"]} rows={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("lists requirements and next commands", () => {
    render(
      <>
        <Requirements items={["ssh and rsync"]} />
        <CommandList title="Then" commands={["rr init"]} />
      </>,
    );
    expect(screen.getByRole("heading", { name: "What you need" })).toBeInTheDocument();
    expect(screen.getByText("rr init")).toBeInTheDocument();
  });
});
