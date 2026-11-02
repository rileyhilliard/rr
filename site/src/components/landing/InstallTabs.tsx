import { type KeyboardEvent, useId, useState } from "react";
import type { InstallOption } from "../../config";
import { track } from "../../lib/analytics";
import { Events } from "../../lib/events";

// The landing page's one island (client:visible). Without JavaScript the first
// command still renders; hydration adds tab switching and the copy button.
export function InstallTabs({ options }: { options: InstallOption[] }) {
  const id = useId();
  const [active, setActive] = useState(0);
  const [copied, setCopied] = useState(false);
  if (options.length === 0) return null;

  const select = (index: number) => {
    setActive(index);
    setCopied(false);
    document.getElementById(`${id}-tab-${index}`)?.focus();
  };
  const onKeyDown = (event: KeyboardEvent) => {
    const step = { ArrowRight: 1, ArrowLeft: -1 }[event.key];
    if (step) select((active + step + options.length) % options.length);
  };
  const copy = async (option: InstallOption) => {
    await navigator.clipboard.writeText(option.command);
    setCopied(true);
    track(Events.installCopied, { method: option.label });
  };

  return (
    <div className="border-line-strong bg-panel rounded-panel border">
      <div role="tablist" aria-label="Install with" className="border-line flex border-b">
        {options.map((option, index) => (
          <button
            key={option.label}
            id={`${id}-tab-${index}`}
            type="button"
            role="tab"
            aria-selected={index === active}
            aria-controls={`${id}-panel-${index}`}
            tabIndex={index === active ? 0 : -1}
            onClick={() => select(index)}
            onKeyDown={onKeyDown}
            className="aria-selected:text-ink aria-selected:border-accent text-ink-2 -mb-px border-b-2 border-transparent px-4 py-2 text-sm font-medium"
          >
            {option.label}
          </button>
        ))}
      </div>
      {options.map((option, index) => (
        <div
          key={option.label}
          id={`${id}-panel-${index}`}
          role="tabpanel"
          aria-labelledby={`${id}-tab-${index}`}
          hidden={index !== active}
          className="flex items-center justify-between gap-4 p-4"
        >
          <code className="text-ink min-w-0 font-mono text-[13px] break-all">
            <span aria-hidden="true" className="text-accent select-none">
              ${" "}
            </span>
            {option.command}
          </code>
          <button
            type="button"
            onClick={() => copy(option)}
            className="border-line-strong text-ink-2 hover:text-ink rounded-control shrink-0 border px-2 py-0.5 text-xs"
          >
            {copied ? "Copied" : "Copy"}
          </button>
        </div>
      ))}
    </div>
  );
}
