import type { VirtualMachine } from "@/api";
import AppActionIconButton from "@/components/ui/AppActionIconButton";

import type { VMAction } from "./vmShared";

export interface VMActionHandlers {
  onDelete: (vm: VirtualMachine) => void;
  onEdit: (vm: VirtualMachine) => void;
  onOpenConsole: (vm: VirtualMachine) => void;
  onRunAction: (action: VMAction, vm: VirtualMachine) => void;
}

const POWER_ACTION: Record<
  string,
  { action: VMAction; icon: string; label: string }
> = {
  paused: { action: "resume", icon: "mdi:play-pause", label: "Resume" },
  running: { action: "shutdown", icon: "mdi:stop", label: "Shutdown" },
};
const START_ACTION = { action: "start", icon: "mdi:play", label: "Start" };

/**
 * One VM's actions as an icon strip, like a container card's. Actions that do
 * not apply to the current state stay visible but disabled, so every strip
 * has the same buttons in the same places.
 */
export default function VMActions({
  compact = false,
  handlers,
  pendingAction,
  vm,
}: {
  /** Smaller buttons for a grid card's header. */
  compact?: boolean;
  handlers: VMActionHandlers;
  pendingAction?: VMAction;
  vm: VirtualMachine;
}) {
  const running = vm.state === "running";
  const live = running || vm.state === "paused";
  const shutOff = vm.state === "shutoff";
  const busy = Boolean(pendingAction);
  const power = POWER_ACTION[vm.state] ?? START_ACTION;
  const buttonSize = compact ? 24 : undefined;
  const actions = [
    {
      action: power.action,
      disabled: busy,
      icon: power.icon,
      label: power.label,
    },
    {
      action: "reboot",
      disabled: busy || !running,
      icon: "mdi:restart",
      label: "Reboot",
    },
    {
      action: "suspend",
      disabled: busy || !running,
      icon: "mdi:pause",
      label: "Suspend",
    },
  ] as const;

  return (
    <div
      aria-busy={busy}
      style={{
        alignItems: "center",
        display: "flex",
        flexWrap: "wrap",
        gap: compact ? 0 : 2,
      }}
    >
      {actions.map((item) => (
        <AppActionIconButton
          ariaLabel={item.label}
          buttonHeight={buttonSize}
          buttonWidth={buttonSize}
          disabled={item.disabled}
          icon={item.icon}
          iconSize={16}
          key={item.label}
          label={item.label}
          loading={pendingAction === item.action}
          onClick={() => handlers.onRunAction(item.action, vm)}
        />
      ))}
      <AppActionIconButton
        ariaLabel="Console"
        buttonHeight={buttonSize}
        buttonWidth={buttonSize}
        disabled={!running || !vm.hasGraphics}
        icon="mdi:monitor"
        iconSize={16}
        label="Console"
        onClick={() => handlers.onOpenConsole(vm)}
      />
      <AppActionIconButton
        ariaLabel="Edit"
        buttonHeight={buttonSize}
        buttonWidth={buttonSize}
        disabled={busy || !shutOff}
        icon="mdi:pencil"
        iconSize={16}
        label={shutOff ? "Edit" : "Shut off the VM to edit it"}
        onClick={() => handlers.onEdit(vm)}
      />
      <AppActionIconButton
        ariaLabel="Force off"
        buttonHeight={buttonSize}
        buttonWidth={buttonSize}
        color="var(--app-palette-error-main)"
        disabled={busy || !live}
        icon="mdi:power"
        iconSize={16}
        label="Force off"
        loading={pendingAction === "force_off"}
        onClick={() => handlers.onRunAction("force_off", vm)}
      />
      <AppActionIconButton
        ariaLabel="Delete"
        buttonHeight={buttonSize}
        buttonWidth={buttonSize}
        color="var(--app-palette-error-main)"
        disabled={busy}
        icon="mdi:trash-can-outline"
        iconSize={16}
        label="Delete"
        onClick={() => handlers.onDelete(vm)}
      />
    </div>
  );
}
