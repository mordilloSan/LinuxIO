import type {
  DragEndEvent,
  DragPendingEvent,
  DragStartEvent,
} from "@dnd-kit/core";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { REORDER_IDLE_EXIT_MS } from "@/constants/reorder";

const configMocks = vi.hoisted(() => ({
  layoutOrders: undefined as Record<string, string[]> | undefined,
  setLayoutOrders: vi.fn(),
}));

vi.mock("@/hooks/useConfig", () => ({
  useConfigValue: vi.fn(() => [
    configMocks.layoutOrders,
    configMocks.setLayoutOrders,
  ]),
}));

const { useReorderableSurface } = await import("@/hooks/useReorderableSurface");
const { act, renderHook } = await import("@/test/render");

interface Item {
  id: string;
}

const getId = (item: Item) => item.id;
const items: Item[] = [{ id: "a" }, { id: "b" }, { id: "c" }];

const dragStart = (id: string, activatorEvent?: Event) =>
  ({ active: { id }, activatorEvent }) as unknown as DragStartEvent;

const dragEnd = (activeId: string, overId: string) =>
  ({
    active: { id: activeId },
    over: { id: overId },
  }) as unknown as DragEndEvent;

const renderSurface = (surfaceItems: Item[] = items) =>
  renderHook(() =>
    useReorderableSurface({ getId, items: surfaceItems, surface: "test" }),
  );

describe("useReorderableSurface", () => {
  beforeEach(() => {
    configMocks.layoutOrders = undefined;
    configMocks.setLayoutOrders.mockReset();
  });

  it("keeps the natural order when nothing was saved", () => {
    const { result } = renderSurface();

    expect(result.current.ids).toEqual(["a", "b", "c"]);
    expect(result.current.editMode).toBe(false);
  });

  it("applies a saved order, drops vanished items and appends new ones", () => {
    configMocks.layoutOrders = { test: ["c", "gone", "a"] };

    const { result } = renderSurface();

    expect(result.current.ids).toEqual(["c", "a", "b"]);
  });

  it("ignores another surface's saved order", () => {
    configMocks.layoutOrders = { other: ["c", "b", "a"] };

    const { result } = renderSurface();

    expect(result.current.ids).toEqual(["a", "b", "c"]);
  });

  it("persists the moved order under its own surface key", () => {
    configMocks.layoutOrders = { other: ["x"] };

    const { result } = renderSurface();
    act(() => {
      result.current.dndContextProps.onDragEnd(dragEnd("a", "c"));
    });

    const update = configMocks.setLayoutOrders.mock.calls[0][0] as (
      previous: Record<string, string[]> | undefined,
    ) => Record<string, string[]>;

    expect(update(configMocks.layoutOrders)).toEqual({
      other: ["x"],
      test: ["b", "c", "a"],
    });
  });

  it("does not write when a drag ends where it started", () => {
    const { result } = renderSurface();

    act(() => {
      result.current.dndContextProps.onDragEnd(dragEnd("b", "b"));
    });

    expect(configMocks.setLayoutOrders).not.toHaveBeenCalled();
  });

  it("opens layout mode when the hold completes into a drag", () => {
    const { result } = renderSurface();

    act(() => {
      result.current.dndContextProps.onDragPending({
        id: "a",
      } as unknown as DragPendingEvent);
    });
    expect(result.current.pendingId).toBe("a");
    expect(result.current.editMode).toBe(false);

    act(() => {
      result.current.dndContextProps.onDragStart(dragStart("a"));
    });

    expect(result.current.editMode).toBe(true);
    expect(result.current.pendingId).toBeNull();
  });

  it("clears the pending item when the hold is abandoned", () => {
    const { result } = renderSurface();

    act(() => {
      result.current.dndContextProps.onDragPending({
        id: "a",
      } as unknown as DragPendingEvent);
      result.current.dndContextProps.onDragAbort();
    });

    expect(result.current.pendingId).toBeNull();
    expect(result.current.editMode).toBe(false);
  });

  it("leaves layout mode after the idle timeout", () => {
    vi.useFakeTimers();
    try {
      const { result } = renderSurface();

      act(() => {
        result.current.dndContextProps.onDragStart(dragStart("a"));
        result.current.dndContextProps.onDragEnd(dragEnd("a", "a"));
      });
      expect(result.current.editMode).toBe(true);

      act(() => {
        vi.advanceTimersByTime(REORDER_IDLE_EXIT_MS - 1);
      });
      expect(result.current.editMode).toBe(true);

      act(() => {
        vi.advanceTimersByTime(1);
      });
      expect(result.current.editMode).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it("restarts the idle timeout on pointer movement", () => {
    vi.useFakeTimers();
    try {
      const { result } = renderSurface();

      act(() => {
        result.current.dndContextProps.onDragStart(dragStart("a"));
        result.current.dndContextProps.onDragEnd(dragEnd("a", "a"));
      });

      act(() => {
        vi.advanceTimersByTime(REORDER_IDLE_EXIT_MS - 100);
        window.dispatchEvent(new Event("pointermove"));
        vi.advanceTimersByTime(REORDER_IDLE_EXIT_MS - 100);
      });
      expect(result.current.editMode).toBe(true);

      act(() => {
        vi.advanceTimersByTime(100);
      });
      expect(result.current.editMode).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it("stays open while a drag is still in flight", () => {
    vi.useFakeTimers();
    try {
      const { result } = renderSurface();

      act(() => {
        result.current.dndContextProps.onDragStart(dragStart("a"));
      });
      act(() => {
        vi.advanceTimersByTime(REORDER_IDLE_EXIT_MS * 2);
      });

      expect(result.current.editMode).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("leaves layout mode on Escape", () => {
    const { result } = renderSurface();

    act(() => {
      result.current.dndContextProps.onDragStart(dragStart("a"));
      result.current.dndContextProps.onDragEnd(dragEnd("a", "a"));
    });
    expect(result.current.editMode).toBe(true);

    act(() => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });

    expect(result.current.editMode).toBe(false);
  });

  it("leaves focus ownership untouched when Escape exits layout mode", () => {
    const pressed = document.createElement("button");
    document.body.append(pressed);
    pressed.focus();

    try {
      const { result } = renderSurface();

      act(() => {
        result.current.dndContextProps.onDragStart(dragStart("a"));
        result.current.dndContextProps.onDragEnd(dragEnd("a", "a"));
      });
      expect(document.activeElement).toBe(pressed);

      act(() => {
        window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
      });

      expect(document.activeElement).toBe(pressed);
    } finally {
      pressed.remove();
    }
  });

  it("reports no layout mode while disabled", () => {
    const { result } = renderHook(() =>
      useReorderableSurface({
        disabled: true,
        getId,
        items,
        surface: "test",
      }),
    );

    act(() => {
      result.current.dndContextProps.onDragStart(dragStart("a"));
    });

    expect(result.current.editMode).toBe(false);
    expect(result.current.pendingId).toBeNull();
  });
});
