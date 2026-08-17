import type { ReactNode } from "react";

import AppButton from "@/components/ui/AppButton";

import "./SelectableCard.css";

interface SelectableCardProps {
  children: ReactNode;
  label: string;
  onOpen?: () => void;
}

/**
 * Opens card details with the standard button interaction. A focused card is
 * rendered statically so its own action buttons are never nested controls.
 */
const SelectableCard = ({
  children,
  label,
  onOpen,
}: SelectableCardProps) => (
  <div className="selectable-card-shell">
    {onOpen ? (
      <AppButton
        aria-label={`Open ${label} details`}
        className="selectable-card-button"
        onClick={onOpen}
      >
        {children}
      </AppButton>
    ) : (
      <div className="selectable-card-static">{children}</div>
    )}
  </div>
);

export default SelectableCard;
