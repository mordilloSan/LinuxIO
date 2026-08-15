import { motion, type MotionValue } from "motion/react";
import type { ReactNode } from "react";

import { useDockMagnification } from "./useDockMagnification";

interface DockActionSlotProps {
  /** A header action control (icon button plus its popup) shown as a tile. */
  children: ReactNode;
  gradient: readonly [string, string];
  mouseX: MotionValue<number>;
}

/* Dresses a header action button as a dock tile: same gradient square, same
   cursor magnification as the nav items. The control keeps its own behavior
   (tooltip, dropdown, dialog); CSS stretches it to fill the tile.

   Unlike nav tiles there is no lift transform: the wrapped dropdowns anchor
   absolutely-/fixed-positioned panels inside this subtree, and a transformed
   ancestor would become their containing block and misplace them. Size-only
   magnification animates width/height, which is transform-free. */
const DockActionSlot = ({
  children,
  gradient,
  mouseX,
}: DockActionSlotProps) => {
  const { size, tileRef } = useDockMagnification(mouseX);

  return (
    <div className="app-dock-link app-dock__action">
      <span className="app-dock__dot" />
      <motion.span
        className="app-dock__tile"
        ref={tileRef}
        style={{
          background: `linear-gradient(180deg, ${gradient[0]}, ${gradient[1]})`,
          height: size,
          width: size,
        }}
      >
        {children}
      </motion.span>
    </div>
  );
};

export default DockActionSlot;
