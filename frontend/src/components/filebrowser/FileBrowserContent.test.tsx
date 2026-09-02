import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import FileBrowserContent, {
  type FileBrowserContentProps,
} from "@/components/filebrowser/FileBrowserContent";
import { render } from "@/test/render";

const headerSearchRender = vi.hoisted(() => vi.fn());

vi.mock("@/components/ui/AppHeaderSearch", () => ({
  default: () => {
    headerSearchRender();
    return null;
  },
}));
vi.mock("./IndexerDialog", () => ({ default: () => null }));
vi.mock("@/hooks/backgroundTasks/useIsIndexing", () => ({
  useIsIndexing: () => false,
}));
vi.mock("@/hooks/backgroundTasks/useBackgroundTaskActions", () => ({
  useBackgroundTaskActions: () => ({
    openIndexerDialog: vi.fn(),
    startIndexer: vi.fn(),
  }),
}));

const contentProps: FileBrowserContentProps = {
  breadcrumbs: <div>Root</div>,
  chrome: {
    editingPath: null,
    isSavingFile: false,
    normalizedPath: "/",
    onOpenDirectory: vi.fn(),
    onSearchCaseSensitiveChange: vi.fn(),
    onSearchChange: vi.fn(),
    onSortChange: vi.fn(),
    onSwitchView: vi.fn(),
    onToggleHiddenFiles: vi.fn(),
    searchQuery: "",
    searchCaseSensitive: false,
    showHiddenFiles: false,
    sortOrder: "asc",
    viewMode: "list",
  },
  data: {
    isSearchLoading: false,
  },
  file: {
    onDownloadCurrent: vi.fn(),
    onEditFile: vi.fn(),
  },
  listing: {
    contextMenuOpen: false,
    cutPaths: new Set(),
    onCancelRename: vi.fn(),
    onConfirmRename: vi.fn(),
    onDelete: vi.fn(),
    onDownloadFile: vi.fn(),
    onOpenDirectory: vi.fn(),
    onSelectedPathsChange: vi.fn(),
    onStartRename: vi.fn(),
    renamingPath: null,
    selectedPaths: new Set(),
    showHiddenFiles: false,
    sortField: "name",
    sortOrder: "asc",
    viewMode: "list",
  },
  surface: {
    isDragOver: false,
    onContextMenu: vi.fn(),
    onDragEnter: vi.fn(),
    onDragLeave: vi.fn(),
    onDragOver: vi.fn(),
    onDrop: vi.fn(),
  },
};

describe("FileBrowserContent render boundaries", () => {
  it("keeps the header stable for data-only renders and path navigation", () => {
    headerSearchRender.mockClear();
    const { rerender } = render(<FileBrowserContent {...contentProps} />);

    expect(headerSearchRender).toHaveBeenCalledTimes(1);

    rerender(
      <FileBrowserContent {...contentProps} data={{ ...contentProps.data }} />,
    );

    expect(headerSearchRender).toHaveBeenCalledTimes(1);

    rerender(
      <FileBrowserContent
        {...contentProps}
        chrome={{ ...contentProps.chrome, normalizedPath: "/next/" }}
      />,
    );

    expect(headerSearchRender).toHaveBeenCalledTimes(1);
  });

  it("reports search failures instead of rendering an empty listing", () => {
    render(
      <FileBrowserContent
        {...contentProps}
        chrome={{ ...contentProps.chrome, searchQuery: "alpha" }}
        data={{
          ...contentProps.data,
          searchError: new Error("indexer request failed"),
        }}
      />,
    );

    expect(screen.getByText("Search unavailable")).toBeInTheDocument();
    expect(screen.getByText("indexer request failed")).toBeInTheDocument();
  });
});
