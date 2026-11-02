// Section 6: the commands you'd actually type, each with its real captured output.
// Without JavaScript the tabs are links and CSS :target shows the linked panel
// (the first by default), so #cmd-doctor deep-links. Hydrated, a tab switches in
// place and its terminal plays.
import { type KeyboardEvent, type MouseEvent, useEffect, useState } from "react";
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

const panelId = (name: string) => `cmd-${name}`;

export function CommandTabs({ commands }: { commands: CommandDemo[] }) {
  const [active, setActive] = useState(0);
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    const fromHash = commands.findIndex((cmd) => `#${panelId(cmd.name)}` === window.location.hash);
    if (fromHash >= 0) setActive(fromHash);
    setHydrated(true);
  }, [commands]);

  if (commands.length === 0) return null;

  const select = (index: number, focus = false) => {
    const cmd = commands[index];
    if (!cmd) return;
    setActive(index);
    history.replaceState(null, "", `#${panelId(cmd.name)}`);
    window.dispatchEvent(new CustomEvent("rr:command-tab", { detail: { name: cmd.name } }));
    if (focus) document.getElementById(`tab-${panelId(cmd.name)}`)?.focus();
  };
  const onClick = (event: MouseEvent, index: number) => {
    event.preventDefault();
    select(index);
  };
  const onKeyDown = (event: KeyboardEvent) => {
    const step = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 }[event.key];
    if (!step) return;
    event.preventDefault();
    select((active + step + commands.length) % commands.length, true);
  };

  return (
    <div
      className={`cmds grid grid-cols-[minmax(0,1fr)] gap-4 lg:grid-cols-[16rem_minmax(0,1fr)] ${hydrated ? "cmds-live" : ""}`}
    >
      <div
        role="tablist"
        aria-label="Commands"
        aria-orientation="vertical"
        className="flex flex-col gap-1"
      >
        {commands.map((cmd, i) => (
          <a
            key={cmd.name}
            id={`tab-${panelId(cmd.name)}`}
            href={`#${panelId(cmd.name)}`}
            role="tab"
            aria-selected={hydrated ? i === active : undefined}
            aria-controls={panelId(cmd.name)}
            tabIndex={hydrated && i !== active ? -1 : 0}
            onClick={(event) => onClick(event, i)}
            onKeyDown={onKeyDown}
            className="rounded-control aria-selected:border-line-strong aria-selected:bg-panel group border border-transparent px-3 py-2 no-underline hover:no-underline"
          >
            <code className="text-ink block font-mono text-[12px]">{cmd.label ?? cmd.command}</code>
            <span className="text-muted group-aria-selected:text-ink-2 mt-0.5 block text-xs">
              {cmd.caption}
            </span>
          </a>
        ))}
      </div>
      <div>
        {commands.map((cmd, i) => {
          const steps: Step[] = [
            { kind: "command", text: cmd.command },
            { kind: "output", lines: cmd.lines.map((text) => ({ text, tone: toneOf(text) })) },
          ];
          const shown = !hydrated || i === active;
          return (
            <section
              key={cmd.name}
              id={panelId(cmd.name)}
              role="tabpanel"
              aria-labelledby={`tab-${panelId(cmd.name)}`}
              hidden={hydrated && i !== active}
              className="cmd-panel scroll-mt-24"
            >
              {shown && (
                <TerminalPlayer
                  key={hydrated ? `${cmd.name}-live` : cmd.name}
                  title={cmd.command}
                  steps={steps}
                  label={`Output of ${cmd.command}`}
                />
              )}
            </section>
          );
        })}
      </div>
    </div>
  );
}
