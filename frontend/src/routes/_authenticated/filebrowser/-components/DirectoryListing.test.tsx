import { beforeEach, describe, expect, it, vi } from "vitest";

import { dragMadeHere } from "@/test/fileBrowserDrag";
import { fireEvent, render, screen } from "@/test/render";

import { INTERNAL_DRAG_TYPE, TRASH_DRAG_TYPE } from "../-dragTypes";
import { setInternalDrag, setTrashDrag } from "../-internalDrag";
import type { FileResource } from "../-types";
import DirectoryListing from "./DirectoryListing";

const mocks = vi.hoisted(() => ({ toastError: vi.fn() }));

vi.mock("sonner", async (importOriginal) => ({
  ...(await importOriginal<typeof import("sonner")>()),
  toast: { error: mocks.toastError },
}));

// Another server's LinuxIO, in another tab.
const OTHER = "https://backup.example:8090";

const carrying = (type: string, payload: unknown) => ({
  dropEffect: "none",
  getData: (requested: string) =>
    requested === type ? JSON.stringify(payload) : "",
  types: [type],
});

beforeEach(() => mocks.toastError.mockClear());

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

vi.mock("@/hooks/useGridColumnCount", () => ({
  useGridColumnCount: () => 1,
}));

vi.mock("../-hooks/useFileSubfolders", () => ({
  useFileSubfolders: () => ({
    error: null,
    isLoading: false,
    isUnavailable: false,
    subfoldersMap: new Map(),
  }),
}));

const resource: FileResource = {
  items: [
    { name: "photos", path: "/srv/projects/photos/", type: "directory" },
    { name: "a.txt", path: "/srv/projects/a.txt", size: 1, type: "file" },
  ],
  name: "projects",
  path: "/srv/projects",
  type: "directory",
};

const noop = () => {};

const renderListing = () => {
  const onDropItems = vi.fn();
  const onRestoreItems = vi.fn();
  render(
    <DirectoryListing
      cardSize={3}
      cutPaths={new Set()}
      isContextMenuOpen={false}
      onCancelRename={noop}
      onConfirmRename={noop}
      onDownloadFile={noop}
      onDropItems={onDropItems}
      onNavigateUp={noop}
      onOpenDirectory={noop}
      onRestoreItems={onRestoreItems}
      onSelectedPathsChange={noop}
      onStartRename={noop}
      renamingPath={null}
      resource={resource}
      selectedPaths={new Set()}
      showHiddenFiles={false}
      sortField="name"
      sortOrder="asc"
      viewMode="list"
    />,
  );
  return { onDropItems, onRestoreItems };
};

const folderRow = () =>
  screen
    .getByText("photos")
    .closest<HTMLElement>("[data-file-path='/srv/projects/photos/']")!;

describe("DirectoryListing drops", () => {
  it("restores items dragged out of the Trash into the folder they are dropped on", () => {
    const { onDropItems, onRestoreItems } = renderListing();
    const items = [
      { name: "report.pdf", path: "/home/a/.local/share/Trash/files/r.pdf" },
    ];
    const dataTransfer = carrying(
      TRASH_DRAG_TYPE,
      dragMadeHere((started) => setTrashDrag(started, items)),
    );

    fireEvent.dragOver(folderRow(), { dataTransfer });
    expect(dataTransfer.dropEffect).toBe("move");
    fireEvent.drop(folderRow(), { dataTransfer });

    expect(onRestoreItems).toHaveBeenCalledWith(items, "/srv/projects/photos/");
    expect(onDropItems).not.toHaveBeenCalled();
  });

  it("moves items dragged from a folder as before", () => {
    const { onDropItems, onRestoreItems } = renderListing();

    fireEvent.drop(folderRow(), {
      dataTransfer: carrying(
        INTERNAL_DRAG_TYPE,
        dragMadeHere((started) =>
          setInternalDrag(started, ["/srv/projects/a.txt"]),
        ),
      ),
    });

    // jsdom has no DragEvent, so the Ctrl key (copy) is not part of it here.
    expect(onDropItems.mock.calls[0]?.slice(0, 2)).toEqual([
      ["/srv/projects/a.txt"],
      "/srv/projects/photos/",
    ]);
    expect(onRestoreItems).not.toHaveBeenCalled();
    expect(mocks.toastError).not.toHaveBeenCalled();
  });

  it("refuses items dragged from another server's tab, either kind", () => {
    const { onDropItems, onRestoreItems } = renderListing();

    fireEvent.drop(folderRow(), {
      dataTransfer: carrying(INTERNAL_DRAG_TYPE, {
        origin: OTHER,
        paths: ["/srv/projects/a.txt"],
      }),
    });
    fireEvent.drop(folderRow(), {
      dataTransfer: carrying(TRASH_DRAG_TYPE, {
        items: [{ name: "r.pdf", path: "/home/a/.local/share/Trash/files/r" }],
        origin: OTHER,
      }),
    });

    expect(onDropItems).not.toHaveBeenCalled();
    expect(onRestoreItems).not.toHaveBeenCalled();
    expect(mocks.toastError).toHaveBeenCalledTimes(2);
    expect(mocks.toastError).toHaveBeenCalledWith("Cannot drop 1 item here", {
      description: `It comes from ${OTHER}, another server. Drop it in a tab of that server.`,
    });
  });
});
