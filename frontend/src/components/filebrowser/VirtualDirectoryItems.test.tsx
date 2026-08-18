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
      }
    | undefined,
  options: undefined as
    | { getItemKey: (index: number) => string | number }
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
    });
  },
}));

vi.mock("@/hooks/useGridColumnCount", () => ({
  useGridColumnCount: () => 3,
}));

vi.mock("@/components/filebrowser/SelectionBox", () => ({
  default: () => null,
}));

vi.mock("@/components/filebrowser/VirtualDirectoryRows", () => ({
  DirectoryItem: () => null,
  SectionHeader: () => null,
}));

const { default: VirtualDirectoryItems } =
  await import("@/components/filebrowser/VirtualDirectoryItems");
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
});
