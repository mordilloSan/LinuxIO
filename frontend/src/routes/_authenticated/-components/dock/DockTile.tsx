import { motion, type MotionStyle } from "motion/react";
import type { ReactNode } from "react";

import { useDockMagnification } from "./useDockMagnification";

interface DockTileProps {
  /** Tile face: a nav icon, or a relocated header control. */
  children: ReactNode;
  gradient: readonly [string, string];
  /** Hover tooltip text, anchored to the tile so it tracks magnification. */
  label: ReactNode;
}

/* The one visual for everything in the dock: running-app dot, gradient tile
   with cursor magnification, and the hover label. The slot keeps flex geometry
   fixed while the visible parts move on compositor transforms. */
const DockTile = ({ children, gradient, label }: DockTileProps) => {
  const { labelY, lift, registerTile, renderScale, x } = useDockMagnification();

  return (
    <span className="app-dock__slot" ref={registerTile}>
      <motion.span className="app-dock__dot" style={{ x }} />
      <motion.span
        className="app-dock__tile"
        style={
          {
            /* The gradient is assembled in CSS from these two, so that
               .app-dock-link--active can substitute its own pair for the route
               you are on without the tile knowing it is active. MotionStyle
               does not admit custom properties, which motion itself passes
               straight through. */
            "--dock-tile-top": gradient[0],
            "--dock-tile-bottom": gradient[1],
            scale: renderScale,
            x,
            y: lift,
          } as MotionStyle
        }
      >
        {children}
      </motion.span>
      <motion.span
        aria-hidden="true"
        className="app-dock__label-anchor"
        style={{ x, y: labelY }}
      >
        <span className="app-dock__label">{label}</span>
      </motion.span>
    </span>
  );
};

export default DockTile;
