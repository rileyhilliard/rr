import { InstallTabs as LibraryInstallTabs } from "@steamfield/ui/react/install-tabs";
import type { InstallOption } from "../../config";
import { track } from "../../lib/analytics";
import { Events } from "../../lib/events";

/** Tab values become ids, so each label is reduced to id-safe characters. */
const idOf = (label: string) => label.toLowerCase().replace(/[^a-z0-9_-]+/g, "-");

// The landing page's install island (client:visible), on @steamfield/ui's InstallTabs.
// Without JavaScript the first command renders and the copy button stays hidden;
// hydration adds tab switching and copying. Its look lives in src/styles/sf-theme.css.
export function InstallTabs({ options }: { options: InstallOption[] }) {
  if (options.length === 0) return null;
  const byId = new Map(options.map((option) => [idOf(option.label), option]));
  return (
    <LibraryInstallTabs
      className="rr-install"
      asAnchors={false}
      options={[...byId].map(([id, option]) => ({ id, ...option }))}
      onCopy={(id, ok) => {
        const option = byId.get(id);
        if (ok && option) track(Events.installCopied, { method: option.label });
      }}
    />
  );
}
