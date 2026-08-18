import {
  closestCenter,
  type CollisionDetection,
  type DragEndEvent,
  type DragOverEvent,
  type DragPendingEvent,
  type DragStartEvent,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { arrayMove, sortableKeyboardCoordinates } from "@dnd-kit/sortable";
import {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
} from "react";

import {
  REORDER_HOLD_MS,
  REORDER_HOLD_TOLERANCE_PX,
  REORDER_IDLE_EXIT_MS,
} from "@/constants/reorder";
import { useConfigValue } from "@/hooks/useConfig";

export interface ReorderableSurfaceOptions<TItem> {
  /**
   * Stable identity for each item. Pass a module-level or memoized function:
   * the merged order is recomputed whenever this changes.
   */
  getId: (item: TItem) => string;
  items: readonly TItem[];
  /**
   * Surface id shared with `useViewMode`, e.g. "docker.containers". This is the
   * key the order is persisted under, so it must stay stable across releases.
   */
  surface: string;
  /**
   * Leaves the surface unarmed — no hold, no drag. Use it when a competing
   * order is in charge (an active column sort) or the list is read-only.
   */
  disabled?: boolean;
  /**
   * Maps a finished drag onto the next saved order, for surfaces whose layout
   * renders composite sortables — a stack band whose drag id stands for a
   * block of member ids. Called with the current ids and the raw active/over
   * ids; return null to keep the default single-id move. Read through a ref,
   * so it doesn't need a stable identity.
   */
  resolveDragEnd?: (
    ids: readonly string[],
    activeId: string,
    overId: string,
  ) => string[] | null;
}

/** Props for the `DndContext` that wraps a reorderable surface. */
export interface ReorderableSurfaceDndProps {
  /**
   * `closestCenter` from the hook. A caller may substitute its own — the
   * stack-band grid wraps it to pin collisions to the drag-start rects, so its
   * reflow preview can never feed back into the collision that drives it.
   */
  collisionDetection: CollisionDetection;
  onDragAbort: () => void;
  onDragCancel: () => void;
  onDragEnd: (event: DragEndEvent) => void;
  /**
   * The hook leaves this unset. A grid whose sortables vary in size (the
   * stack-band grid) previews a drag by re-rendering with the provisional
   * order instead of with strategy transforms — dnd-kit's supported pattern
   * for variable sizes — and layers its own handler onto the returned props.
   */
  onDragOver?: (event: DragOverEvent) => void;
  onDragPending: (event: DragPendingEvent) => void;
  onDragStart: (event: DragStartEvent) => void;
  sensors: ReturnType<typeof useSensors>;
}

export interface ReorderableSurface<TItem> {
  /** Spread onto the `DndContext` that wraps the surface. */
  dndContextProps: ReorderableSurfaceDndProps;
  /** True while the surface accepts drags — handles and overlays show. */
  editMode: boolean;
  exitEditMode: () => void;
  /** `SortableContext` items, in the same order as `items`. */
  ids: string[];
  /** `options.items` in the user's saved order. */
  items: TItem[];
  /** The item currently being held, before the hold completes. */
  pendingId: string | null;
}

/**
 * Merges a saved order with the live list: items the server no longer reports
 * drop out, and items the user has never positioned land at the end. Neither
 * case needs a config write, so a stale saved order stays harmless.
 */
function applySavedOrder<TItem>(
  items: readonly TItem[],
  savedOrder: readonly string[] | undefined,
  getId: (item: TItem) => string,
): TItem[] {
  if (!savedOrder?.length) return [...items];

  const remaining = new Map(items.map((item) => [getId(item), item]));
  const ordered: TItem[] = [];

  for (const id of savedOrder) {
    const item = remaining.get(id);
    if (item !== undefined) {
      ordered.push(item);
      remaining.delete(id);
    }
  }

  return [...ordered, ...remaining.values()];
}

/**
 * Drag-to-reorder for one list, entered by holding a card or row rather than by
 * a toolbar button.
 *
 * The hold is dnd-kit's own delay constraint, so the gesture that opens layout
 * mode is the same gesture that picks the item up — by the time the mode turns
 * on at `onDragStart`, the held item is already moving. dnd-kit also swallows
 * the trailing click and clears the text selection on activation, so the hold
 * cannot double as a row click or smear a selection across the list.
 */
export function useReorderableSurface<TItem>({
  disabled = false,
  getId,
  items,
  resolveDragEnd,
  surface,
}: ReorderableSurfaceOptions<TItem>): ReorderableSurface<TItem> {
  const [layoutOrders, setLayoutOrders] = useConfigValue("layoutOrders");
  const [editMode, setEditMode] = useState(false);
  const [pendingId, setPendingId] = useState<string | null>(null);
  const isDraggingRef = useRef(false);
  const resolveDragEndRef = useRef(resolveDragEnd);
  useEffect(() => {
    resolveDragEndRef.current = resolveDragEnd;
  }, [resolveDragEnd]);
  // Read inside the sensor callback so the sensor descriptors never change
  // identity: dnd-kit resolves them while a press is already in flight.
  const editModeRef = useRef(false);
  const isEditing = editMode && !disabled;
  useEffect(() => {
    editModeRef.current = isEditing;
  }, [isEditing]);

  const savedOrder = layoutOrders?.[surface];
  const orderedItems = useMemo(
    () => applySavedOrder(items, savedOrder, getId),
    [getId, items, savedOrder],
  );
  const ids = useMemo(
    () => orderedItems.map((item) => getId(item)),
    [getId, orderedItems],
  );

  const sensorOptions = useMemo(
    () => ({
      activationConstraint: {
        delay: REORDER_HOLD_MS,
        tolerance: REORDER_HOLD_TOLERANCE_PX,
      },
      // Once layout mode is open the hold has been paid for: further drags start
      // on movement alone.
      bypassActivationConstraint: () => editModeRef.current,
    }),
    [],
  );
  const sensors = useSensors(
    useSensor(MouseSensor, sensorOptions),
    useSensor(TouchSensor, sensorOptions),
    // The REORDER_HOLD_MS hold is a mouse gesture with no keyboard equivalent.
    // The keyboard sensor is the way in without one: focus a card, press
    // Space, use arrows.
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const exitEditMode = useCallback(() => {
    isDraggingRef.current = false;
    setPendingId(null);
    setEditMode(false);
  }, []);

  const handleDragPending = useCallback((event: DragPendingEvent) => {
    setPendingId(String(event.id));
  }, []);

  const handleDragStart = useCallback(() => {
    isDraggingRef.current = true;
    setPendingId(null);
    setEditMode(true);
  }, []);

  const handleDragAborted = useCallback(() => {
    isDraggingRef.current = false;
    setPendingId(null);
  }, []);

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      isDraggingRef.current = false;
      setPendingId(null);

      const { active, over } = event;
      if (!over || active.id === over.id) return;

      const activeId = String(active.id);
      const overId = String(over.id);
      const resolvedOrder = resolveDragEndRef.current?.(ids, activeId, overId);
      if (resolvedOrder) {
        setLayoutOrders((previous) => ({
          ...(previous ?? {}),
          [surface]: resolvedOrder,
        }));
        return;
      }

      const oldIndex = ids.indexOf(activeId);
      const newIndex = ids.indexOf(overId);
      if (oldIndex < 0 || newIndex < 0) return;

      const nextOrder = arrayMove(ids, oldIndex, newIndex);
      setLayoutOrders((previous) => ({
        ...(previous ?? {}),
        [surface]: nextOrder,
      }));
    },
    [ids, setLayoutOrders, surface],
  );

  // Layout mode is a transient state, not a setting: it closes itself once the
  // user stops interacting, and Escape closes it immediately.
  const closeIfIdle = useEffectEvent(() => {
    if (isDraggingRef.current) return;
    exitEditMode();
  });

  useEffect(() => {
    if (!isEditing) return;

    let timeoutId = window.setTimeout(closeIfIdle, REORDER_IDLE_EXIT_MS);
    const restartIdleTimer = () => {
      window.clearTimeout(timeoutId);
      timeoutId = window.setTimeout(closeIfIdle, REORDER_IDLE_EXIT_MS);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        exitEditMode();
        return;
      }
      restartIdleTimer();
    };

    window.addEventListener("pointermove", restartIdleTimer, { passive: true });
    window.addEventListener("pointerdown", restartIdleTimer, { passive: true });
    window.addEventListener("wheel", restartIdleTimer, { passive: true });
    window.addEventListener("keydown", handleKeyDown);

    return () => {
      window.clearTimeout(timeoutId);
      window.removeEventListener("pointermove", restartIdleTimer);
      window.removeEventListener("pointerdown", restartIdleTimer);
      window.removeEventListener("wheel", restartIdleTimer);
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [isEditing, exitEditMode]);

  // Memoized because memoized tables compare this by identity.
  const dndContextProps = useMemo<ReorderableSurfaceDndProps>(
    () => ({
      collisionDetection: closestCenter,
      onDragAbort: handleDragAborted,
      onDragCancel: handleDragAborted,
      onDragEnd: handleDragEnd,
      onDragPending: handleDragPending,
      onDragStart: handleDragStart,
      sensors,
    }),
    [
      handleDragAborted,
      handleDragEnd,
      handleDragPending,
      handleDragStart,
      sensors,
    ],
  );

  return {
    dndContextProps,
    editMode: isEditing,
    exitEditMode,
    ids,
    items: orderedItems,
    pendingId: disabled ? null : pendingId,
  };
}
