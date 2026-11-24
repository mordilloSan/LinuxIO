import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import FileBrowserContent, {
  type FileBrowserContentProps,
} from "./FileBrowserContent";

const headerSearchRender = vi.hoisted(() => vi.fn());

// The listing reports the card size it was given.
vi.mock("./DirectoryListing", () => ({
  default: ({ cardSize }: { cardSize: number }) => (
    <div data-card-size={cardSize} data-testid="listing" />
  ),
}));

vi.mock("@/components/ui/AppHeaderSearch", () => ({
  default: () => {
    headerSearchRender();
    return null;
  },
}));
vi.mock("./IndexerDialog", () => ({ default: () => null }));
vi.mock("@/tasks/useIsIndexing", () => ({
  useIsIndexing: () => false,
}));
vi.mock("@/tasks/useBackgroundTaskActions", () => ({
  useBackgroundTaskActions: () => ({
    openIndexerDialog: vi.fn(),
    startIndexer: vi.fn(),
  }),
}));

const contentProps: FileBrowserContentProps = {
  breadcrumbs: <div>Root</div>,
  chrome: {
    cardSize: 6,
    editingPath: null,
    onCardSizeChange: vi.fn(),
    isSavingFile: false,
    normalizedPath: "/",
    onDropItems: vi.fn(),
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
    onDeletePermanently: vi.fn(),
    onDownloadFile: vi.fn(),
    onDropItems: vi.fn(),
    onNavigateUp: vi.fn(),
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
    const { rerender } = render(<FileBrowserContent {...contentProps} />, {
      auth: { privileged: true },
    });

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

describe("FileBrowserContent search of a folder on disk", () => {
  const folder = {
    items: [{ name: "a.txt", path: "/srv/a.txt", type: "file" }],
    name: "srv",
    path: "/srv",
    type: "directory",
  };
  const note =
    "This folder could not be searched on disk completely, so recent changes may be missing.";
  const searching = (
    data: Partial<FileBrowserContentProps["data"]>,
  ): FileBrowserContentProps => ({
    ...contentProps,
    chrome: { ...contentProps.chrome, searchQuery: "alpha" },
    data: {
      ...contentProps.data,
      filteredResource: folder,
      resource: folder,
      ...data,
    },
  });

  it("says so when the folder could not be read whole", () => {
    render(<FileBrowserContent {...searching({ isSearchPartial: true })} />);

    expect(screen.getByText(note)).toBeInTheDocument();
    expect(screen.getByTestId("listing")).toBeInTheDocument();
  });

  it("says nothing when the folder was read whole or not read", () => {
    render(<FileBrowserContent {...searching({ isSearchPartial: false })} />);

    expect(screen.queryByText(note)).not.toBeInTheDocument();
  });
});

describe("FileBrowserContent card size", () => {
  const folder = {
    items: [{ name: "a.txt", path: "/srv/a.txt", type: "file" }],
    name: "srv",
    path: "/srv",
    type: "directory",
  };
  const cardView = (
    overrides: Partial<FileBrowserContentProps["data"]> = {},
  ): FileBrowserContentProps => ({
    ...contentProps,
    chrome: { ...contentProps.chrome, viewMode: "card" },
    data: {
      ...contentProps.data,
      filteredResource: folder,
      resource: folder,
      ...overrides,
    },
    listing: { ...contentProps.listing, viewMode: "card" },
  });

  // jsdom lays nothing out: the area the listing fills is this wide.
  let areaWidth = 1200;
  beforeEach(() => {
    areaWidth = 1200;
    const width = vi
      .spyOn(HTMLElement.prototype, "clientWidth", "get")
      .mockImplementation(() => areaWidth);
    return () => width.mockRestore();
  });

  it("puts the slider beside the view button and lists cards at its size", () => {
    const onCardSizeChange = vi.fn();
    const props = cardView();
    render(
      <FileBrowserContent
        {...props}
        chrome={{ ...props.chrome, onCardSizeChange }}
      />,
    );

    const slider = screen.getByRole("slider", { name: "Card size" });
    expect(slider).toHaveValue("6");
    // In the header's actions, before the view button.
    const actions = document.querySelector(".file-browser-header__actions");
    const toggle = screen.getByRole("button", { name: "Switch to list view" });
    expect(actions).toContainElement(slider);
    expect(
      slider.compareDocumentPosition(toggle) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(screen.getByTestId("listing")).toHaveAttribute(
      "data-card-size",
      "6",
    );

    fireEvent.change(slider, { target: { value: "2" } });
    expect(onCardSizeChange).toHaveBeenCalledWith(2);
  });

  it("offers the slider in an empty folder", () => {
    const empty = { ...folder, items: [] };
    render(
      <FileBrowserContent
        {...cardView({ filteredResource: empty, resource: empty })}
      />,
    );

    expect(screen.getByRole("slider", { name: "Card size" })).toBeVisible();
  });

  it("has no slider in list view", () => {
    render(<FileBrowserContent {...contentProps} />);

    expect(screen.queryByRole("slider")).not.toBeInTheDocument();
  });

  it("hides the slider and shows the default size where one card fills a row", () => {
    areaWidth = 379;
    render(<FileBrowserContent {...cardView()} />);

    expect(screen.queryByRole("slider")).not.toBeInTheDocument();
    expect(screen.getByTestId("listing")).toHaveAttribute(
      "data-card-size",
      "3",
    );
  });
});
