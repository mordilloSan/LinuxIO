import { motion, type MotionValue } from "motion/react";
import type { ReactNode } from "react";

import { useDockMagnification } from "./useDockMagnification";

interface DockTileProps {
  /** Tile face: a nav icon, or a relocated header control. */
  children: ReactNode;
  gradient: readonly [string, string];
  /** Hover tooltip text, anchored to the tile so it tracks magnification. */
  label: ReactNode;
  mouseX: MotionValue<number>;
}

/* The one visual for everything in the dock: running-app dot, gradient tile
   with cursor magnification, and the hover label.

   The dip is applied via `top` instead of a translate transform: relocated
   header controls anchor absolutely-/fixed-positioned dropdown panels inside
   the tile subtree, and a transformed ancestor would become their containing
   block and misplace them. `top` moves the tile identically but stays
   transform-free. */
const DockTile = ({ children, gradient, label, mouseX }: DockTileProps) => {
  const { lift, size, tileRef } = useDockMagnification(mouseX);

  return (
    <>
      <span className="app-dock__dot" />
      <motion.span
        className="app-dock__tile"
        ref={tileRef}
        style={{
          background: `linear-gradient(180deg, ${gradient[0]}, ${gradient[1]})`,
          height: size,
          top: lift,
          width: size,
        }}
      >
        {children}
        <span className="app-dock__label">{label}</span>
      </motion.span>
    </>
  );
};

export default DockTile;
