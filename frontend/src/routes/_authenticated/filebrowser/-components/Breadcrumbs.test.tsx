import { beforeEach, describe, expect, it, vi } from "vitest";

import { dragMadeHere } from "@/test/fileBrowserDrag";
import { fireEvent, render, screen } from "@/test/render";

import { INTERNAL_DRAG_TYPE, TRASH_DRAG_TYPE } from "../-dragTypes";
import { setInternalDrag, setTrashDrag } from "../-internalDrag";
import Breadcrumbs from "./Breadcrumbs";

const mocks = vi.hoisted(() => ({ toastError: vi.fn() }));

vi.mock("sonner", async (importOriginal) => ({
  ...(await importOriginal<typeof import("sonner")>()),
  toast: { error: mocks.toastError },
}));

const transfer = (type: string, data: unknown) => ({
  dropEffect: "none",
  getData: (requested: string) =>
    requested === type ? JSON.stringify(data) : "",
  types: [type],
});

// Another server's LinuxIO, in another tab.
const OTHER = "https://backup.example:8090";

// A drag names its items, the server it came from and its token: a drag
// started on this page, made just before its drop, or a drag of another
// server, which has a token of its own.
const ITEMS = [
  { name: "report.pdf", path: "/home/alice/.local/share/Trash/files/r.pdf" },
];
const trashed = () =>
  dragMadeHere((dataTransfer) => setTrashDrag(dataTransfer, ITEMS));
const moved = (paths: string[], origin?: string) =>
  origin
    ? { origin, paths, token: "f".repeat(32) }
    : dragMadeHere((dataTransfer) => setInternalDrag(dataTransfer, paths));

describe("Breadcrumbs", () => {
  beforeEach(() => mocks.toastError.mockClear());

  it("keeps the browser from opening desktop files dropped on the trail", () => {
    const onDropItems = vi.fn();
    const onRestoreItems = vi.fn();
    const onPlaceDrop = vi.fn();
    render(
      <Breadcrumbs
        onDropItems={onDropItems}
        onNavigate={vi.fn()}
        onRestoreItems={onRestoreItems}
        path="/srv/projects"
        place={{
          icon: "mdi:trash-can-outline",
          label: "Trash",
          onDropItems: onPlaceDrop,
        }}
      />,
    );
    const files = () => ({
      dropEffect: "copy",
      getData: () => "",
      types: ["Files"],
    });

    for (const name of ["breadcrumb-link-srv", "breadcrumb-link-Trash"]) {
      const target = screen.getByRole("button", { name });
      const dataTransfer = files();
      // fireEvent answers false when the handler prevented the default.
      expect(fireEvent.dragOver(target, { dataTransfer })).toBe(false);
      expect(dataTransfer.dropEffect).toBe("none");
      expect(fireEvent.drop(target, { dataTransfer: files() })).toBe(false);
    }
    expect(onDropItems).not.toHaveBeenCalled();
    expect(onRestoreItems).not.toHaveBeenCalled();
    expect(onPlaceDrop).not.toHaveBeenCalled();
  });

  it("refuses items dragged from another server's tab onto a segment", () => {
    const onDropItems = vi.fn();
    const onRestoreItems = vi.fn();
    render(
      <Breadcrumbs
        onDropItems={onDropItems}
        onNavigate={vi.fn()}
        onRestoreItems={onRestoreItems}
        path="/srv/projects"
      />,
    );
    const srv = screen.getByRole("button", { name: "breadcrumb-link-srv" });

    fireEvent.drop(srv, {
      dataTransfer: transfer(
        INTERNAL_DRAG_TYPE,
        moved(["/srv/projects/a.txt"], OTHER),
      ),
    });
    fireEvent.drop(srv, {
      dataTransfer: transfer(TRASH_DRAG_TYPE, {
        items: ITEMS,
        origin: OTHER,
        token: "f".repeat(32),
      }),
    });

    expect(onDropItems).not.toHaveBeenCalled();
    expect(onRestoreItems).not.toHaveBeenCalled();
    expect(mocks.toastError).toHaveBeenCalledTimes(2);
    expect(mocks.toastError).toHaveBeenCalledWith("Cannot drop 1 item here", {
      description: `It comes from ${OTHER}, another server. Drop it in a tab of that server.`,
    });
  });

  it("restores items dragged out of the Trash into the segment they are dropped on", () => {
    const onDropItems = vi.fn();
    const onRestoreItems = vi.fn();
    render(
      <Breadcrumbs
        onDropItems={onDropItems}
        onNavigate={vi.fn()}
        onRestoreItems={onRestoreItems}
        path="/srv/projects"
      />,
    );
    const srv = screen.getByRole("button", { name: "breadcrumb-link-srv" });
    const dataTransfer = transfer(TRASH_DRAG_TYPE, trashed());

    fireEvent.dragOver(srv, { dataTransfer });
    expect(dataTransfer.dropEffect).toBe("move");
    fireEvent.drop(srv, { dataTransfer });

    expect(onRestoreItems).toHaveBeenCalledWith(ITEMS, "/srv");
    // Never a move: the trash's own name and .trashinfo would stay behind.
    expect(onDropItems).not.toHaveBeenCalled();
  });

  it("still moves items dragged from a folder", () => {
    const onDropItems = vi.fn();
    const onRestoreItems = vi.fn();
    render(
      <Breadcrumbs
        onDropItems={onDropItems}
        onNavigate={vi.fn()}
        onRestoreItems={onRestoreItems}
        path="/srv/projects"
      />,
    );
    const root = screen.getByRole("button", { name: "Go to root" });

    fireEvent.drop(root, {
      dataTransfer: transfer(
        INTERNAL_DRAG_TYPE,
        moved(["/srv/projects/a.txt"]),
      ),
    });

    // jsdom has no DragEvent, so the Ctrl key (copy) is not part of it here.
    expect(onDropItems.mock.calls[0]?.slice(0, 2)).toEqual([
      ["/srv/projects/a.txt"],
      "/",
    ]);
    expect(onRestoreItems).not.toHaveBeenCalled();
  });

  it("ends the trail with a place that takes items to the Trash", () => {
    const onPlaceDrop = vi.fn();
    const onNavigate = vi.fn();
    render(
      <Breadcrumbs
        onNavigate={onNavigate}
        onRestoreItems={vi.fn()}
        path="/srv/projects"
        place={{
          icon: "mdi:trash-can-outline",
          label: "Trash",
          onDropItems: onPlaceDrop,
        }}
      />,
    );
    const trash = screen.getByRole("button", { name: "breadcrumb-link-Trash" });
    expect(trash).toHaveAttribute("aria-current", "page");

    // The folder segments lead back to the folders.
    const projects = screen.getByRole("button", {
      name: "breadcrumb-link-projects",
    });
    expect(projects).not.toHaveClass("active");
    fireEvent.click(projects);
    expect(onNavigate).toHaveBeenCalledWith("/srv/projects");

    fireEvent.drop(trash, {
      dataTransfer: transfer(
        INTERNAL_DRAG_TYPE,
        moved(["/srv/projects/a.txt"]),
      ),
    });
    expect(onPlaceDrop).toHaveBeenCalledWith(["/srv/projects/a.txt"]);

    // Items already in the Trash are not dropped onto it.
    onPlaceDrop.mockClear();
    fireEvent.drop(trash, {
      dataTransfer: transfer(TRASH_DRAG_TYPE, trashed()),
    });
    expect(onPlaceDrop).not.toHaveBeenCalled();

    // Nor are items dragged from another server's tab.
    fireEvent.drop(trash, {
      dataTransfer: transfer(
        INTERNAL_DRAG_TYPE,
        moved(["/srv/projects/a.txt", "/srv/b.txt"], OTHER),
      ),
    });
    expect(onPlaceDrop).not.toHaveBeenCalled();
    expect(mocks.toastError).toHaveBeenCalledWith("Cannot drop 2 items here", {
      description: `They come from ${OTHER}, another server. Drop them in a tab of that server.`,
    });
  });
});
