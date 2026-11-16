import { describe, expect, it, vi } from "vitest";

import { fireEvent, render, screen } from "@/test/render";

import { INTERNAL_DRAG_TYPE, TRASH_DRAG_TYPE } from "../-dragTypes";
import Breadcrumbs from "./Breadcrumbs";

const transfer = (type: string, data: unknown) => ({
  dropEffect: "none",
  getData: (requested: string) =>
    requested === type ? JSON.stringify(data) : "",
  types: [type],
});

const TRASHED = [
  { name: "report.pdf", path: "/home/alice/.local/share/Trash/files/r.pdf" },
];

describe("Breadcrumbs", () => {
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
    const dataTransfer = transfer(TRASH_DRAG_TYPE, TRASHED);

    fireEvent.dragOver(srv, { dataTransfer });
    expect(dataTransfer.dropEffect).toBe("move");
    fireEvent.drop(srv, { dataTransfer });

    expect(onRestoreItems).toHaveBeenCalledWith(TRASHED, "/srv");
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
      dataTransfer: transfer(INTERNAL_DRAG_TYPE, ["/srv/projects/a.txt"]),
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
      dataTransfer: transfer(INTERNAL_DRAG_TYPE, ["/srv/projects/a.txt"]),
    });
    expect(onPlaceDrop).toHaveBeenCalledWith(["/srv/projects/a.txt"]);

    // Items already in the Trash are not dropped onto it.
    onPlaceDrop.mockClear();
    fireEvent.drop(trash, { dataTransfer: transfer(TRASH_DRAG_TYPE, TRASHED) });
    expect(onPlaceDrop).not.toHaveBeenCalled();
  });
});
