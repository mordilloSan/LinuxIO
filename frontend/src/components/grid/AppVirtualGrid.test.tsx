import { describe, expect, it, vi } from "vitest";

const virtualizerState = vi.hoisted(() => ({
  listeners: new Set<() => void>(),
  version: 0,
}));

vi.mock("@tanstack/react-virtual", async () => {
  const { useSyncExternalStore } =
    await vi.importActual<typeof import("react")>("react");

  return {
    useVirtualizer: ({
      count,
      getItemKey,
    }: {
      count: number;
      getItemKey: (index: number) => string | number;
    }) => {
      useSyncExternalStore(
        (listener) => {
          virtualizerState.listeners.add(listener);
          return () => virtualizerState.listeners.delete(listener);
        },
        () => virtualizerState.version,
        () => virtualizerState.version,
      );

      return {
        getTotalSize: () => count * 80,
        getVirtualItems: () =>
          Array.from({ length: count }, (_, index) => ({
            end: (index + 1) * 80,
            index,
            key: getItemKey(index),
            lane: 0,
            size: 80,
            start: index * 80,
          })),
        measure: vi.fn(),
        measureElement: vi.fn(),
        scrollToIndex: vi.fn(),
      };
    },
  };
});

const AppVirtualGrid = (await import("@/components/grid/AppVirtualGrid"))
  .default;
const { act, render } = await import("@/test/render");

describe("AppVirtualGrid", () => {
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
});
