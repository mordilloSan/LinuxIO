import {
  closestCenter,
  type CollisionDetection,
  pointerWithin,
} from "@dnd-kit/core";

/**
 * A stack's sortable rect includes every visible member row. Using that full
 * rect's centre makes an upward drag change targets only after the header has
 * travelled far past a short row. The pointer stays on the header, so it is the
 * precise target signal; closestCenter remains the non-pointer/keyboard fallback.
 */
export const containerTableCollisionDetection: CollisionDetection = (args) => {
  const pointerCollisions = pointerWithin(args).filter(
    ({ id }) => id !== args.active.id,
  );
  return pointerCollisions.length > 0
    ? pointerCollisions
    : closestCenter(args);
};
