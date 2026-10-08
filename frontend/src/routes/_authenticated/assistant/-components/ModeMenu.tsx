import type * as acp from "@agentclientprotocol/sdk";
import { Icon } from "@iconify/react";
import { useState } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTooltip from "@/components/ui/AppTooltip";

const MODE_ICONS: Record<string, string> = {
  default: "mdi:hand-back-right-outline",
  acceptEdits: "mdi:code-tags",
  plan: "mdi:script-text-outline",
  bypassPermissions: "mdi:lightning-bolt-outline",
};

export const modeIcon = (id: string) => MODE_ICONS[id] ?? "mdi:tune";

/** The current mode as a dim button; opens upward into a list of every mode. */
export default function ModeMenu({
  modes,
  disabled,
  onSetMode,
}: {
  modes: acp.SessionModeState;
  disabled: boolean;
  onSetMode: (modeId: string) => void;
}) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const current = modes.availableModes.find(
    (mode) => mode.id === modes.currentModeId,
  );
  const name = current?.name ?? modes.currentModeId;

  return (
    <>
      <AppTooltip title="Mode (Shift+Tab to cycle)">
        <AppIconButton
          aria-haspopup="menu"
          aria-keyshortcuts="Shift+Tab"
          aria-label={`Mode: ${name}`}
          className="assistant__bar-button"
          color="secondary"
          disabled={disabled}
          onClick={(event) => setAnchor(event.currentTarget)}
        >
          <Icon height={16} icon={modeIcon(modes.currentModeId)} width={16} />
          {name}
        </AppIconButton>
      </AppTooltip>
      <AppMenu
        anchorEl={anchor}
        anchorOrigin={{ vertical: "top", horizontal: "right" }}
        ariaLabel="Modes"
        className="assistant__mode-menu"
        onClose={() => setAnchor(null)}
        open={anchor !== null}
        transformOrigin={{ vertical: "bottom", horizontal: "right" }}
      >
        <div className="assistant__menu-header" role="presentation">
          Modes
        </div>
        {modes.availableModes.map((mode) => (
          <AppMenuItem
            endAdornment={
              mode.id === modes.currentModeId ? <Icon icon="mdi:check" /> : null
            }
            key={mode.id}
            onClick={() => {
              setAnchor(null);
              onSetMode(mode.id);
            }}
            selected={mode.id === modes.currentModeId}
            startAdornment={<Icon icon={modeIcon(mode.id)} />}
          >
            <span className="assistant__mode-name">{mode.name}</span>
            {mode.description ? (
              <span className="assistant__mode-description">
                {mode.description}
              </span>
            ) : null}
          </AppMenuItem>
        ))}
      </AppMenu>
    </>
  );
}
