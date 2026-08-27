import { mixWithTransparency } from "@/theme/surfaces";

interface SelectionBoxProps {
  height: number;
  left: number;
  top: number;
  width: number;
}

/**
 * Visual overlay for marquee selection box
 */
const SelectionBox = ({ left, top, width, height }: SelectionBoxProps) => {
  return (
    <div
      style={{
        position: "absolute",
        left: `${left}px`,
        top: `${top}px`,
        width: `${width}px`,
        height: `${height}px`,
        border: "2px solid var(--app-palette-primary-main)",
        backgroundColor: mixWithTransparency(
          "var(--app-palette-primary-main)",
          0.1,
        ),
        pointerEvents: "none",
        zIndex: 1000,
        borderRadius: "4px",
      }}
    />
  );
};

export default SelectionBox;
