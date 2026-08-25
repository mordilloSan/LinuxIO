import { describe, expect, it, vi } from "vitest";

import type { FileItem } from "@/types/filebrowser";

const virtualizerState = vi.hoisted(() => ({
  measure: vi.fn(),
  instance: undefined as
    | {
        containerRef: () => void;
        getVirtualItems: () => never[];
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
      getVirtualItems: () => [],
      measure: virtualizerState.measure,
      measureElement: () => {},
      scrollToIndex: vi.fn(),
    });
  },
}));

vi.mock("@/hooks/useGridColumnCount", () => ({
  useGridColumnCount: () => 3,
}));

vi.mock("@/components/filebrowser/SelectionBox", () => ({
  default: () => null,
}));

vi.mock("@/components/filebrowser/DirectoryRows", () => ({
  DirectoryItem: () => null,
  SectionHeader: () => null,
}));

const {
  createDirectoryLayout,
  default: VirtualDirectoryItems,
  getDirectoryRevealRowIndex,
  getDirectoryVirtualRow,
  getDirectoryVirtualRowKey,
} = await import("@/components/filebrowser/VirtualDirectoryItems");
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
