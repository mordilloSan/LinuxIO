import { fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { FileItem } from "../-types";

const virtualizerState = vi.hoisted(() => ({
  gridOptions: undefined as { minItemWidth: number } | undefined,
  items: [] as { index: number; key: string }[],
  measure: vi.fn(),
  instance: undefined as
    | {
        containerRef: () => void;
        getVirtualItems: () => { index: number; key: string }[];
        measure: ReturnType<typeof vi.fn>;
        measureElement: () => void;
        scrollToIndex: ReturnType<typeof vi.fn>;
      }
    | undefined,
  options: undefined as
    | {
        count: number;
        estimateSize: (index: number) => number;
        getItemKey: (index: number) => string | number;
      }
    | undefined,
}));

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: (options: typeof virtualizerState.options) => {
    virtualizerState.options = options;
    return (virtualizerState.instance ??= {
      containerRef: () => {},
      getVirtualItems: () => virtualizerState.items,
      measure: virtualizerState.measure,
      measureElement: () => {},
      scrollToIndex: vi.fn(),
    });
  },
}));

vi.mock("@/hooks/useGridColumnCount", () => ({
  // Three columns up to the default card width, two for wider cards.
  useGridColumnCount: (_ref: unknown, options: { minItemWidth: number }) => {
    virtualizerState.gridOptions = options;
    return options.minItemWidth > 260 ? 2 : 3;
  },
}));

vi.mock("./SelectionBox", () => ({
  default: () => null,
}));

vi.mock("./DirectoryRows", () => ({
  DirectoryItem: () => null,
  SectionHeader: ({ label }: { label: string }) => <h6>{label}</h6>,
}));

const {
  cardMinWidth,
  createDirectoryLayout,
  default: VirtualDirectoryItems,
  getDirectoryRevealRowIndex,
  getDirectoryVirtualRow,
  getDirectoryVirtualRowKey,
  VirtualItems,
} = await import("./VirtualDirectoryItems");
type ItemPosition = import("./VirtualDirectoryItems").ItemPosition;
const { render } = await import("@/test/render");

const item = (index: number): FileItem => ({
  name: `file-${index}.txt`,
  path: `/files/file-${index}.txt`,
  type: "file",
});

const baseProps = {
  containerRef: { current: null },
  cutPaths: new Set<string>(),
  folders: [],
  isLoadingSubfolders: false,
  isMarqueeSelecting: false,
  onCancelRename: vi.fn(),
  onConfirmRename: vi.fn(),
  onContainerMouseDown: vi.fn(),
  onDownloadFile: vi.fn(),
  onFileClick: vi.fn(),
  onFileContextMenu: vi.fn(),
  onFolderClick: vi.fn(),
  onFolderContextMenu: vi.fn(),
  onMarqueeMouseDown: vi.fn(),
  onOpenDirectory: vi.fn(),
  renamingPath: null,
  revealIndex: -1,
  selectedPaths: new Set<string>(),
  selectionBox: null,
  subfoldersMap: new Map(),
  viewMode: "list" as const,
};

describe("VirtualDirectoryItems virtualizer invalidation", () => {
  it("does not clear measured rows when the item count changes", () => {
    virtualizerState.measure.mockClear();
    const view = render(
      <VirtualDirectoryItems {...baseProps} files={[item(0), item(1)]} />,
    );

    expect(virtualizerState.measure).toHaveBeenCalledTimes(1);
    const firstKey = virtualizerState.options?.getItemKey(1);

    view.rerender(
      <VirtualDirectoryItems
        {...baseProps}
        files={[item(0), item(1), item(2), item(3)]}
      />,
    );

    expect(virtualizerState.measure).toHaveBeenCalledTimes(1);
    expect(virtualizerState.options?.getItemKey(1)).toBe(firstKey);
  });

  it("remeasures when the row layout mode changes", () => {
    virtualizerState.measure.mockClear();
    const view = render(
      <VirtualDirectoryItems {...baseProps} files={[item(0), item(1)]} />,
    );

    view.rerender(
      <VirtualDirectoryItems
        {...baseProps}
        files={[item(0), item(1)]}
        viewMode="card"
      />,
    );

    expect(virtualizerState.measure).toHaveBeenCalledTimes(2);
  });

  it("estimates card rows from their resting rendered geometry", () => {
    render(
      <VirtualDirectoryItems
        {...baseProps}
        files={[item(0), item(1)]}
        viewMode="card"
      />,
    );

    expect(virtualizerState.options?.estimateSize(0)).toBe(40);
    expect(virtualizerState.options?.estimateSize(1)).toBe(104);
  });

  it("estimates list rows from their resting rendered geometry", () => {
    render(
      <VirtualDirectoryItems
        {...baseProps}
        files={[item(0), item(1)]}
        viewMode="list"
      />,
    );

    expect(virtualizerState.options?.estimateSize(1)).toBe(42);
  });

  it("scrolls a revealed file directly to its virtual row", () => {
    const folders = [item(0), item(1)];
    const files = [item(2), item(3), item(4), item(5)];

    virtualizerState.instance?.scrollToIndex.mockClear();
    render(
      <VirtualDirectoryItems
        {...baseProps}
        files={files}
        folders={folders}
        revealIndex={5}
        viewMode="card"
      />,
    );

    expect(virtualizerState.instance?.scrollToIndex).toHaveBeenCalledWith(4, {
      align: "auto",
    });
  });
});

describe("VirtualDirectoryItems card size", () => {
  const storageKey = "linuxio.filebrowserCardSize";
  const cardProps = {
    ...baseProps,
    files: [item(0), item(1)],
    viewMode: "card" as const,
  };

  beforeEach(() => {
    window.localStorage.clear();
    virtualizerState.items = [{ index: 0, key: "header" }];
    return () => {
      virtualizerState.items = [];
    };
  });

  it("starts at the default width and offers the slider on the first header", () => {
    render(<VirtualDirectoryItems {...cardProps} />);

    expect(virtualizerState.gridOptions?.minItemWidth).toBe(260);
    expect(screen.getByRole("slider", { name: "Card size" })).toHaveValue("3");
  });

  it("changes the narrowest card width and remembers the size", () => {
    render(<VirtualDirectoryItems {...cardProps} />);

    fireEvent.change(screen.getByRole("slider", { name: "Card size" }), {
      target: { value: "7" },
    });

    expect(virtualizerState.gridOptions?.minItemWidth).toBe(cardMinWidth(7));
    expect(cardMinWidth(7)).toBe(420);
    expect(cardMinWidth(1)).toBe(180);
    expect(window.localStorage.getItem(storageKey)).toBe("7");
  });

  it("starts from the remembered size and ignores one out of range", () => {
    window.localStorage.setItem(storageKey, "5");
    const view = render(<VirtualDirectoryItems {...cardProps} />);
    expect(virtualizerState.gridOptions?.minItemWidth).toBe(cardMinWidth(5));
    view.unmount();

    window.localStorage.setItem(storageKey, "12");
    render(<VirtualDirectoryItems {...cardProps} />);
    expect(virtualizerState.gridOptions?.minItemWidth).toBe(260);
  });

  it("does not start a marquee from the slider", () => {
    const onMarqueeMouseDown = vi.fn();
    render(
      <VirtualDirectoryItems
        {...cardProps}
        onMarqueeMouseDown={onMarqueeMouseDown}
      />,
    );

    fireEvent.mouseDown(screen.getByRole("slider", { name: "Card size" }));

    expect(onMarqueeMouseDown).not.toHaveBeenCalled();
  });

  it("does not scroll to the focused item when the size changes the layout", () => {
    render(
      <VirtualDirectoryItems
        {...cardProps}
        files={[item(0), item(1), item(2), item(3), item(4)]}
        revealIndex={4}
      />,
    );
    virtualizerState.instance?.scrollToIndex.mockClear();

    fireEvent.change(screen.getByRole("slider", { name: "Card size" }), {
      target: { value: "7" },
    });

    expect(virtualizerState.instance?.scrollToIndex).not.toHaveBeenCalled();
  });

  it("has no slider in list view or on a later header", () => {
    const view = render(
      <VirtualDirectoryItems {...cardProps} viewMode="list" />,
    );
    expect(screen.queryByRole("slider")).not.toBeInTheDocument();
    view.unmount();

    virtualizerState.items = [{ index: 2, key: "files-header" }];
    render(<VirtualDirectoryItems {...cardProps} folders={[item(9)]} />);
    expect(screen.queryByRole("slider")).not.toBeInTheDocument();
  });
});

describe("VirtualItems as a listbox", () => {
  const listbox = (
    viewMode: "card" | "list",
    renderItem = vi.fn(
      (entry: FileItem, _kind: string, position: ItemPosition) => (
        <div
          aria-posinset={position.posInSet}
          aria-selected={false}
          aria-setsize={position.setSize}
          role="option"
        >
          {entry.name}
        </div>
      ),
    ),
  ) => {
    render(
      <VirtualItems
        containerRef={{ current: null }}
        files={[item(0), item(1), item(2), item(3)]}
        folders={[]}
        heading="Items"
        isMarqueeSelecting={false}
        listboxLabel="Trash"
        onContainerMouseDown={vi.fn()}
        onMarqueeMouseDown={vi.fn()}
        renderItem={renderItem}
        revealIndex={-1}
        selectionBox={null}
        viewMode={viewMode}
      />,
    );
    return renderItem;
  };

  beforeEach(() => {
    window.localStorage.clear();
    // The first row is in view; the list has no heading rows.
    virtualizerState.items = [{ index: 0, key: "first-row" }];
    return () => {
      virtualizerState.items = [];
    };
  });

  it("holds only options, each placed in the whole list", () => {
    const renderItem = listbox("list");

    const list = screen.getByRole("listbox", { name: "Trash" });
    expect(list).toHaveAttribute("aria-multiselectable", "true");
    expect(within(list).queryByRole("heading")).not.toBeInTheDocument();
    // Only the first item row is in view, and it knows it is 1 of 4.
    const options = within(list).getAllByRole("option");
    expect(options.map((option) => option.textContent)).toEqual(["file-0.txt"]);
    expect(options[0]).toHaveAttribute("aria-posinset", "1");
    expect(options[0]).toHaveAttribute("aria-setsize", "4");
    expect(renderItem).toHaveBeenCalledWith(item(0), "file", {
      posInSet: 1,
      setSize: 4,
    });
    expect(virtualizerState.options?.count).toBe(4);
    // The heading names the list from outside it.
    expect(screen.getByRole("heading", { name: "Items" })).not.toBe(null);
  });

  it("keeps the heading and the card-size slider outside the listbox", () => {
    listbox("card");

    const list = screen.getByRole("listbox", { name: "Trash" });
    expect(within(list).queryByRole("slider")).not.toBeInTheDocument();
    expect(within(list).queryByRole("heading")).not.toBeInTheDocument();
    expect(screen.getByRole("slider", { name: "Card size" })).not.toBe(null);
    // Three cards a row at the default size: the first row holds three.
    expect(
      within(list)
        .getAllByRole("option")
        .map((option) => option.getAttribute("aria-posinset")),
    ).toEqual(["1", "2", "3"]);
  });
});

describe("VirtualDirectoryItems lazy row layout", () => {
  const folders = [item(0), item(1)];
  const files = [item(2), item(3), item(4), item(5)];

  it("maps section headers and item rows without materializing row items", () => {
    const layout = createDirectoryLayout({
      columnCount: 3,
      fileCount: files.length,
      folderCount: folders.length,
      viewMode: "card",
    });

    expect(layout.totalRowCount).toBe(5);
    expect(getDirectoryVirtualRow(0, layout)).toEqual({
      label: "Folders",
      type: "sectionHeader",
    });
    expect(getDirectoryVirtualRow(1, layout)).toEqual({
      count: 2,
      itemKind: "folder",
      start: 0,
      type: "items",
    });
    expect(getDirectoryVirtualRow(2, layout)).toEqual({
      label: "Files",
      type: "sectionHeader",
    });
    expect(getDirectoryVirtualRow(4, layout)).toEqual({
      count: 1,
      itemKind: "file",
      start: 3,
      type: "items",
    });
  });

  it("keeps row keys and reveal mapping tied to item positions", () => {
    const layout = createDirectoryLayout({
      columnCount: 2,
      fileCount: files.length,
      folderCount: folders.length,
      viewMode: "card",
    });

    expect(getDirectoryVirtualRowKey(1, layout, folders, files)).toContain(
      folders[0].path,
    );
    expect(getDirectoryVirtualRowKey(4, layout, folders, files)).toContain(
      files[3].path,
    );
    expect(getDirectoryRevealRowIndex(0, layout)).toBe(1);
    expect(getDirectoryRevealRowIndex(1, layout)).toBe(1);
    expect(getDirectoryRevealRowIndex(2, layout)).toBe(3);
    expect(getDirectoryRevealRowIndex(5, layout)).toBe(4);
    expect(getDirectoryRevealRowIndex(6, layout)).toBe(-1);
    expect(getDirectoryRevealRowIndex(-1, layout)).toBe(-1);

    const replacedFiles = [...files];
    replacedFiles[1] = { ...replacedFiles[1], path: "/files/replaced.txt" };
    expect(getDirectoryVirtualRowKey(3, layout, folders, files)).not.toBe(
      getDirectoryVirtualRowKey(3, layout, folders, replacedFiles),
    );
  });

  it("lays a list out without heading rows when asked", () => {
    const layout = createDirectoryLayout({
      columnCount: 2,
      fileCount: files.length,
      folderCount: folders.length,
      headers: false,
      viewMode: "card",
    });

    expect(layout.totalRowCount).toBe(3);
    expect(getDirectoryVirtualRow(0, layout)).toEqual({
      count: 2,
      itemKind: "folder",
      start: 0,
      type: "items",
    });
    expect(getDirectoryVirtualRow(2, layout)).toEqual({
      count: 2,
      itemKind: "file",
      start: 2,
      type: "items",
    });
    expect(getDirectoryRevealRowIndex(0, layout)).toBe(0);
    expect(getDirectoryRevealRowIndex(5, layout)).toBe(2);
    expect(getDirectoryVirtualRowKey(1, layout, folders, files)).toContain(
      files[0].path,
    );
  });

  it("omits empty sections while preserving file row indices", () => {
    const layout = createDirectoryLayout({
      columnCount: 2,
      fileCount: files.length,
      folderCount: 0,
      viewMode: "card",
    });

    expect(layout.totalRowCount).toBe(3);
    expect(getDirectoryVirtualRow(0, layout)).toEqual({
      label: "Files",
      type: "sectionHeader",
    });
    expect(getDirectoryRevealRowIndex(0, layout)).toBe(1);
  });
});
