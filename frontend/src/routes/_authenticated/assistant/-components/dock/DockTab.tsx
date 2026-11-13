import { Icon } from "@iconify/react";
import type { Ref } from "react";

import AppButton from "@/components/ui/AppButton";

import "./dock.css";

/**
 * A tab on the chat's left edge that opens and closes its card. It stays in
 * place while the card is open, so the same spot does both.
 */
export default function DockTab({
  label,
  open,
  onClick,
  icon,
  disabled = false,
  running = false,
  quiet = false,
  ref,
  ringRef,
  glowRef,
}: {
  label: string;
  open: boolean;
  onClick: () => void;
  /** Without an icon the tab shows a live dot. */
  icon?: string;
  disabled?: boolean;
  /** The dot pulses while the agent works. */
  running?: boolean;
  /** The dot is grey: nothing it stands for is at work. */
  quiet?: boolean;
  ref?: Ref<HTMLButtonElement>;
  ringRef?: Ref<HTMLSpanElement>;
  glowRef?: Ref<HTMLSpanElement>;
}) {
  return (
    <AppButton
      aria-expanded={open}
      className={[
        "assistant-dock-tab",
        open && "assistant-dock-tab--open",
        running && "assistant-dock-tab--running",
        quiet && "assistant-dock-tab--quiet",
      ]
        .filter(Boolean)
        .join(" ")}
      color="inherit"
      disabled={disabled}
      onClick={onClick}
      ref={ref}
    >
      <span aria-hidden className="assistant-dock-tab__glow" ref={glowRef} />
      {icon ? (
        <Icon aria-hidden icon={icon} width={16} />
      ) : (
        <span aria-hidden className="assistant-dock-tab__live">
          <span className="assistant-dock-tab__ring" ref={ringRef} />
        </span>
      )}
      <span className="assistant-dock-tab__label">{label}</span>
    </AppButton>
  );
}
