import { beforeEach, describe, expect, it, vi } from "vitest";

const virtualizerState = vi.hoisted(() => ({
  listeners: new Set<() => void>(),
  measure: vi.fn(),
  version: 0,
}));

vi.mock("@tanstack/react-virtual", async () => {
  const { useRef, useSyncExternalStore } =
    await vi.importActual<typeof import("react")>("react");

  return {
    useVirtualizer: ({
      count,
      getItemKey,
    }: {
      count: number;
      getItemKey: (index: number) => string | number;
    }) => {
      const optionsRef = useRef({ count, getItemKey });
      optionsRef.current = { count, getItemKey };
      useSyncExternalStore(
        (listener) => {
          virtualizerState.listeners.add(listener);
          return () => virtualizerState.listeners.delete(listener);
        },
        () => virtualizerState.version,
        () => virtualizerState.version,
      );

      const virtualizerRef = useRef<{
        getTotalSize: () => number;
        getVirtualItems: () => Array<{
          end: number;
          index: number;
          key: string | number;
          lane: number;
          size: number;
          start: number;
        }>;
        measure: typeof virtualizerState.measure;
        measureElement: ReturnType<typeof vi.fn>;
        scrollToIndex: ReturnType<typeof vi.fn>;
      } | null>(null);
      if (!virtualizerRef.current) {
        virtualizerRef.current = {
          getTotalSize: () => optionsRef.current.count * 80,
          getVirtualItems: () =>
            Array.from({ length: optionsRef.current.count }, (_, index) => ({
              end: (index + 1) * 80,
              index,
              key: optionsRef.current.getItemKey(index),
              lane: 0,
              size: 80,
              start: index * 80,
            })),
          measure: virtualizerState.measure,
          measureElement: vi.fn(),
          scrollToIndex: vi.fn(),
        };
      }
      return virtualizerRef.current;
    },
  };
});

const AppVirtualGrid = (await import("@/components/grid/AppVirtualGrid"))
  .default;
const { act, render } = await import("@/test/render");

describe("AppVirtualGrid", () => {
  beforeEach(() => {
    virtualizerState.measure.mockClear();
  });

  it("keeps stable visible items out of virtualizer-only updates", () => {
    const items = [{ id: "one" }, { id: "two" }];
    const renderItem = vi.fn((item: { id: string }) => <span>{item.id}</span>);

    render(
      <AppVirtualGrid
        getItemKey={(item) => item.id}
        height={200}
        items={items}
        minItemWidth={1}
        renderItem={renderItem}
      />,
    );

    expect(renderItem).toHaveBeenCalledTimes(2);

    act(() => {
      virtualizerState.version += 1;
      virtualizerState.listeners.forEach((listener) => listener());
    });

    expect(renderItem).toHaveBeenCalledTimes(2);
  });

  it("does not reset measured rows when the item count changes", () => {
    const getItemKey = (item: { id: string }) => item.id;
    const view = render(
      <AppVirtualGrid
        getItemKey={getItemKey}
        height={200}
        items={[{ id: "one" }, { id: "two" }]}
        minItemWidth={1}
        renderItem={(item) => <span>{item.id}</span>}
      />,
    );

    expect(virtualizerState.measure).toHaveBeenCalledTimes(1);

    view.rerender(
      <AppVirtualGrid
        getItemKey={getItemKey}
        height={200}
        items={[{ id: "one" }, { id: "two" }, { id: "three" }]}
        minItemWidth={1}
        renderItem={(item) => <span>{item.id}</span>}
      />,
    );

    expect(virtualizerState.measure).toHaveBeenCalledTimes(1);

    view.rerender(
      <AppVirtualGrid
        estimateItemHeight={100}
        getItemKey={getItemKey}
        height={200}
        items={[{ id: "one" }, { id: "two" }, { id: "three" }]}
        minItemWidth={1}
        renderItem={(item) => <span>{item.id}</span>}
      />,
    );

    expect(virtualizerState.measure).toHaveBeenCalledTimes(1);

    view.rerender(
      <AppVirtualGrid
        estimateItemHeight={100}
        gap={20}
        getItemKey={getItemKey}
        height={200}
        items={[{ id: "one" }, { id: "two" }, { id: "three" }]}
        minItemWidth={1}
        renderItem={(item) => <span>{item.id}</span>}
      />,
    );

    expect(virtualizerState.measure).toHaveBeenCalledTimes(2);
  });
});
