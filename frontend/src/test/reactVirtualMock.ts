import { vi } from "vitest";

/**
 * jsdom has no layout, so the real virtualizer measures a zero-height viewport
 * and mounts no rows. A test that renders an `AppDataTable` (or
 * `AppVirtualGrid`) consumer mocks the virtualizer with this factory so every
 * row mounts — the same rows a real browser shows for lists this small:
 *
 *   vi.mock("@tanstack/react-virtual", async () =>
 *     (await import("@/test/reactVirtualMock")).reactVirtualMock(),
 *   );
 */
export function reactVirtualMock() {
  return {
    useVirtualizer: ({
      count,
      getItemKey,
    }: {
      count: number;
      getItemKey?: (index: number) => string | number;
    }) => ({
      getTotalSize: () => count * 48,
      getVirtualItems: () =>
        Array.from({ length: count }, (_, index) => ({
          end: (index + 1) * 48,
          index,
          key: getItemKey?.(index) ?? index,
          lane: 0,
          size: 48,
          start: index * 48,
        })),
      measure: vi.fn(),
      measureElement: vi.fn(),
      resizeItem: vi.fn(),
      scrollToIndex: vi.fn(),
    }),
  };
}
