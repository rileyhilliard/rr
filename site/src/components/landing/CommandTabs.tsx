// Section 6: the commands you'd actually type, each with its real captured output.
// Without JavaScript the tabs are links and CSS :target shows the linked panel
// (the first by default; @steamfield/ui/tabs/styles.css), so #cmd-doctor deep-links.
// Hydrated, a tab switches in place and its terminal plays from the start.
import { Tab, TabList, TabPanel, Tabs } from "@steamfield/ui/react/tabs";
import { useEffect, useRef } from "react";
import { toneOf } from "../../lib/tone";
import { type Step, TerminalPlayer } from "./TerminalPlayer";

export type CommandDemo = {
  name: string;
  /** Exactly what was run; the terminal shows it. */
  command: string;
  /** The tab's short form, when it differs from the command. */
  label?: string;
  caption: string;
  /** The recording, one string per line; tones are applied here (toneOf). */
  lines: string[];
};

/** The library's pick event; re-sent as rr:command-tab, which src/scripts/site.ts tracks. */
const PICK_EVENT = "rr:command-tab-pick";

export function CommandTabs({ commands }: { commands: CommandDemo[] }) {
  const root = useRef<HTMLDivElement>(null);

  // A reader's pick only: the library fires no event for the tab a URL hash opens.
  useEffect(() => {
    const node = root.current;
    if (!node) return;
    const onPick = (event: Event) => {
      const name = (event as CustomEvent<{ value: string }>).detail.value;
      window.dispatchEvent(new CustomEvent("rr:command-tab", { detail: { name } }));
    };
    node.addEventListener(PICK_EVENT, onPick);
    return () => node.removeEventListener(PICK_EVENT, onPick);
  }, []);

  if (commands.length === 0) return null;

  return (
    <div ref={root}>
      <Tabs
        id="cmd"
        orientation="vertical"
        hash="replace"
        keepMounted={false}
        eventName={PICK_EVENT}
        className="grid grid-cols-[minmax(0,1fr)] gap-4 lg:grid-cols-[16rem_minmax(0,1fr)]"
      >
        <TabList aria-label="Commands" className="flex flex-col gap-1">
          {commands.map((cmd) => (
            <Tab
              key={cmd.name}
              value={cmd.name}
              className="rounded-control aria-selected:border-line-strong aria-selected:bg-panel group border border-transparent px-3 py-2 no-underline hover:no-underline"
            >
              <code className="text-ink block font-mono text-[12px]">
                {cmd.label ?? cmd.command}
              </code>
              <span className="text-muted group-aria-selected:text-ink-2 mt-0.5 block text-xs">
                {cmd.caption}
              </span>
            </Tab>
          ))}
        </TabList>
        <div>
          {commands.map((cmd) => {
            const steps: Step[] = [
              { kind: "command", text: cmd.command },
              { kind: "output", lines: cmd.lines.map((text) => ({ text, tone: toneOf(text) })) },
            ];
            return (
              <TabPanel key={cmd.name} value={cmd.name} className="scroll-mt-24">
                <TerminalPlayer
                  title={cmd.command}
                  steps={steps}
                  label={`Output of ${cmd.command}`}
                />
              </TabPanel>
            );
          })}
        </div>
      </Tabs>
    </div>
  );
}
