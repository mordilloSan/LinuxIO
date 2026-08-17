import type { CollisionDetection } from "@dnd-kit/core";
import { describe, expect, it } from "vitest";

import { containerTableCollisionDetection } from "./containerTableDnd";

type CollisionArgs = Parameters<CollisionDetection>[0];
type CollisionRect = CollisionArgs["collisionRect"];
type DroppableContainer = CollisionArgs["droppableContainers"][number];

const rect = (top: number, height: number): CollisionRect => ({
  bottom: top + height,
  height,
  left: 0,
  right: 100,
  top,
  width: 100,
});

const droppable = (
  id: string,
  collisionRect: CollisionRect,
): DroppableContainer => ({
  data: { current: undefined },
  disabled: false,
  id,
  key: id,
  node: { current: null },
  rect: { current: collisionRect },
});

describe("containerTableCollisionDetection", () => {
  it("uses the row under the pointer instead of the tall stack centre", () => {
    const stackRect = rect(60, 240);
    const targetRect = rect(0, 60);
    const active = droppable("stack:immich", stackRect);
    const target = droppable("pi-hole", targetRect);

    const collisions = containerTableCollisionDetection({
      active: {
        data: active.data,
        id: active.id,
        rect: { current: { initial: stackRect, translated: stackRect } },
      },
      collisionRect: stackRect,
      droppableContainers: [active, target],
      droppableRects: new Map([
        [active.id, stackRect],
        [target.id, targetRect],
      ]),
      pointerCoordinates: { x: 20, y: 30 },
    });

    expect(collisions[0]?.id).toBe("pi-hole");
  });

  it("ignores the active entry when pointer rectangles overlap", () => {
    const sharedRect = rect(0, 60);
    const active = droppable("stack:immich", sharedRect);
    const target = droppable("vaultwarden", sharedRect);

    const collisions = containerTableCollisionDetection({
      active: {
        data: active.data,
        id: active.id,
        rect: { current: { initial: sharedRect, translated: sharedRect } },
      },
      collisionRect: sharedRect,
      droppableContainers: [active, target],
      droppableRects: new Map([
        [active.id, sharedRect],
        [target.id, sharedRect],
      ]),
      pointerCoordinates: { x: 20, y: 30 },
    });

    expect(collisions.map(({ id }) => id)).toContain("vaultwarden");
    expect(collisions.map(({ id }) => id)).not.toContain("stack:immich");
  });
});
