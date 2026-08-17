import type { MouseEvent, ReactNode } from "react";

import AppButton from "@/components/ui/AppButton";

import "./SelectableCard.css";

interface SelectableCardProps {
  children: ReactNode;
  label: string;
  onSelect: (checked: boolean) => void;
  selected: boolean;
}

/**
 * A card whose pointer selection gesture matches selectable table rows.
 * Pointer clicks only focus the card; pointer double-clicks toggle selection.
 * Keyboard and assistive-technology clicks have detail 0, so the native
 * button remains operable with Enter and Space.
 */
const SelectableCard = ({
  children,
  label,
  onSelect,
  selected,
}: SelectableCardProps) => {
  const toggleSelection = () => onSelect(!selected);
  const handleClick = (event: MouseEvent<HTMLButtonElement>) => {
    if (event.detail === 0) {
      toggleSelection();
    }
  };
  const handleDoubleClick = (event: MouseEvent<HTMLButtonElement>) => {
    toggleSelection();
    // Pointer selection is complete. Do not leave the button holding focus:
    // the next Escape would switch :focus-visible on and paint a second,
    // full-card outline over the selected bottom accent. Keyboard activation
    // still keeps its focus because it arrives through handleClick above.
    event.currentTarget.blur();
  };

  return (
    <AppButton
      aria-label={`${selected ? "Deselect" : "Select"} ${label}`}
      aria-pressed={selected}
      className="selectable-card-button"
      onClick={handleClick}
      onDoubleClick={handleDoubleClick}
    >
      {children}
    </AppButton>
  );
};

export default SelectableCard;
