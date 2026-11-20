import { beforeEach, describe, expect, it, vi } from "vitest";

import type { TrashEntry } from "@/api";
import { fireEvent, render, screen } from "@/test/render";

import TrashList from "./TrashList";

// The rows in the virtual window, which the test moves as a scroll would.
const view = vi.hoisted(() => ({ rows: [] as number[] }));

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({
    count,
    getItemKey,
  }: {
    count: number;
    getItemKey?: (index: number) => string | number;
  }) => ({
    containerRef: () => {},
    getVirtualItems: () =>
      view.rows
        .filter((index) => index < count)
        .map((index) => ({
          end: (index + 1) * 48,
          index,
          key: getItemKey?.(index) ?? index,
          lane: 0,
          size: 48,
          start: index * 48,
        })),
    measure: vi.fn(),
    measureElement: () => {},
    scrollToIndex: vi.fn(),
  }),
}));

vi.mock("@/hooks/useGridColumnCount", () => ({
  useGridColumnCount: () => 1,
}));

const TRASH = "/home/alice/.local/share/Trash/files";
const entry = (name: string): TrashEntry => ({
  confirmRestore: false,
  deletedAt: "",
  isDir: false,
  name,
  originalPath: `/home/alice/${name}`,
  path: `${TRASH}/${name}`,
  size: 1,
});
const entries = [entry("a.txt"), entry("b.txt")];

const props = {
  cardSize: 3,
  entries,
  onDelete: vi.fn(),
  onDropped: vi.fn(),
  onLeave: vi.fn(),
  onOpen: vi.fn(),
  onSelectionChange: vi.fn(),
  onSortChange: vi.fn(),
  sortField: "name" as const,
  sortOrder: "asc" as const,
  viewMode: "list" as const,
};

describe("TrashList", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    view.rows = [0, 1];
  });

  it("hears the end of a drag whose row scrolled out of the window", () => {
    const { rerender } = render(
      <TrashList {...props} selectedPaths={new Set()} />,
    );
    const dragged = screen
      .getAllByRole("option")
      .find((option) => option.textContent?.includes("a.txt"))!;
    fireEvent.dragStart(dragged, {
      dataTransfer: { effectAllowed: "", setData: vi.fn() },
    });

    // The list scrolls during the drag and its row leaves the document.
    view.rows = [1];
    rerender(
      <TrashList {...props} selectedPaths={new Set([entries[0].path])} />,
    );
    expect(dragged.isConnected).toBe(false);

    // Dropped where it restores: its item still leaves the selection.
    fireEvent.dragEnd(dragged, { dataTransfer: { dropEffect: "move" } });
    expect(props.onDropped).toHaveBeenCalledTimes(1);
    expect(props.onDropped).toHaveBeenCalledWith([entries[0].path]);
  });

  it("keeps the selection when the drag ends nowhere", () => {
    render(<TrashList {...props} selectedPaths={new Set()} />);
    const dragged = screen.getAllByRole("option")[0];
    fireEvent.dragStart(dragged, {
      dataTransfer: { effectAllowed: "", setData: vi.fn() },
    });

    fireEvent.dragEnd(dragged, { dataTransfer: { dropEffect: "none" } });
    expect(props.onDropped).not.toHaveBeenCalled();
  });
});
