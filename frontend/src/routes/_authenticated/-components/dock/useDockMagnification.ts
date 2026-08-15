import { useSpring, useTransform, type MotionValue } from "motion/react";
import { useRef } from "react";

/* Resting tile size, peak size under the cursor, and how far (px) the
   magnification bulge reaches to either side of the cursor. */
export const DOCK_TILE_SIZE = 40;
export const DOCK_TILE_SIZE_MAX = 64;
const MAGNIFY_RANGE = 140;
/* Positive = downward: tiles hang from the top-mounted bar and dip toward
   the cursor. */
const LIFT_MAX = 8;

const SPRING = { damping: 14, mass: 0.1, stiffness: 170 };

/* Shared magnification physics for anything rendered as a dock tile: the
   tile's size and vertical dip derive from its distance to the cursor. */
export function useDockMagnification(mouseX: MotionValue<number>) {
  const tileRef = useRef<HTMLSpanElement>(null);

  const distance = useTransform(mouseX, (x) => {
    const bounds = tileRef.current?.getBoundingClientRect();
    if (!bounds) return Infinity;
    return x - bounds.x - bounds.width / 2;
  });

  const size = useSpring(
    useTransform(
      distance,
      [-MAGNIFY_RANGE, 0, MAGNIFY_RANGE],
      [DOCK_TILE_SIZE, DOCK_TILE_SIZE_MAX, DOCK_TILE_SIZE],
    ),
    SPRING,
  );
  const lift = useSpring(
    useTransform(
      distance,
      [-MAGNIFY_RANGE, 0, MAGNIFY_RANGE],
      [0, LIFT_MAX, 0],
    ),
    SPRING,
  );

  return { lift, size, tileRef };
}
