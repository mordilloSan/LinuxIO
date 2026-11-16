import { beforeEach, describe, expect, it, vi } from "vitest";

import type { TrashEntry } from "@/api";
import { fireEvent, render, screen, waitFor, within } from "@/test/render";

import { INTERNAL_DRAG_TYPE, TRASH_DRAG_TYPE } from "../-dragTypes";
import type { TrashJobs } from "../-hooks/useTrashJobs";
import TrashPlace from "./TrashPlace";

const mocks = vi.hoisted(() => ({
  entries: [] as unknown[],
  error: null as Error | null,
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        trash_list: {
          queryFn: async () => {
            if (mocks.error) throw mocks.error;
            return { entries: mocks.entries };
          },
          queryKey: ["linuxio", "filebrowser", "trash_list"],
        },
      },
    },
  };
});

const TRASH = "/home/alice/.local/share/Trash/files";
const entry = (fields: Partial<TrashEntry> & { name: string }): TrashEntry => ({
  confirmRestore: false,
  deletedAt: "",
  isDir: false,
  originalPath: "",
  path: `${TRASH}/${fields.name}`,
  size: 0,
  ...fields,
});

const report = entry({
  deletedAt: "2026-01-02T03:04:05Z",
  name: "report.pdf",
  originalPath: "/home/alice/docs/report.pdf",
  size: 2048,
});
const photos = entry({
  deletedAt: "2026-01-01T10:00:00Z",
  isDir: true,
  name: "photos",
  originalPath: "/srv/media/photos",
});
// Trashed under another name; shown, and restored, under its own.
const notes = entry({
  deletedAt: "2026-01-03T08:00:00Z",
  name: "notes.2.txt",
  originalPath: "/home/alice/notes.txt",
  size: 10,
});
const mystery = entry({ name: "mystery.bin", size: 12 });
const all = [report, photos, notes, mystery];

const jobs = {
  purge: vi.fn(),
  restore: vi.fn(),
  restoreInto: vi.fn(),
} satisfies TrashJobs;
const onLeave = vi.fn();
const onTrashItems = vi.fn();

const renderPlace = () =>
  render(
    <TrashPlace
      breadcrumbs={<div>Trail</div>}
      folderPath="/home/alice"
      jobs={jobs}
      onLeave={onLeave}
      onTrashItems={onTrashItems}
    />,
  );

const rowOrder = () =>
  screen
    .getAllByRole("option")
    .map((row) => row.getAttribute("data-file-path"));
const row = (item: TrashEntry) =>
  screen
    .getAllByRole("option")
    .find((option) => option.getAttribute("data-file-path") === item.path)!;
const headerButton = (name: string) =>
  screen.getAllByRole("button", { name })[0];

describe("TrashPlace", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.entries = all;
    mocks.error = null;
  });

  it("lists items under their shown names, newest deleted first", async () => {
    renderPlace();

    await screen.findByRole("listbox", { name: "Trash" });
    expect(rowOrder()).toEqual([
      notes.path,
      report.path,
      photos.path,
      mystery.path,
    ]);
    expect(within(row(notes)).getByText("notes.txt")).toBeVisible();
    expect(within(row(notes)).getByText("/home/alice")).toBeVisible();
    expect(within(row(mystery)).getByText("mystery.bin")).toBeVisible();
    // A trashed folder lists no size; an item of unknown origin says so.
    expect(within(row(mystery)).getAllByText("Unknown")).toHaveLength(2);
    expect(within(row(report)).getByText("2 KB")).toBeVisible();
  });

  it("sorts by each column, ascending then descending", async () => {
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });

    await user.click(screen.getByRole("button", { name: "Sort by Name" }));
    expect(rowOrder()).toEqual([
      mystery.path,
      notes.path,
      photos.path,
      report.path,
    ]);
    await user.click(
      screen.getByRole("button", { name: "Sort by Name, ascending" }),
    );
    expect(rowOrder()).toEqual([
      report.path,
      photos.path,
      notes.path,
      mystery.path,
    ]);
    await user.click(
      screen.getByRole("button", { name: "Sort by Original location" }),
    );
    expect(rowOrder()).toEqual([
      mystery.path,
      notes.path,
      report.path,
      photos.path,
    ]);
    await user.click(screen.getByRole("button", { name: "Sort by Size" }));
    expect(rowOrder()).toEqual([
      photos.path,
      notes.path,
      mystery.path,
      report.path,
    ]);
  });

  it("selects as a folder does: click, Ctrl and Shift click, Ctrl+A, Escape", async () => {
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });
    const selected = () =>
      screen
        .getAllByRole("option")
        .filter((option) => option.getAttribute("aria-selected") === "true")
        .map((option) => option.getAttribute("data-file-path"));

    await user.click(row(notes));
    expect(selected()).toEqual([notes.path]);
    await user.keyboard("{Control>}");
    await user.click(row(photos));
    await user.keyboard("{/Control}");
    expect(selected()).toEqual([notes.path, photos.path]);
    await user.click(row(report));
    await user.keyboard("{Shift>}");
    await user.click(row(mystery));
    await user.keyboard("{/Shift}");
    expect(selected()).toEqual([report.path, photos.path, mystery.path]);
    await user.keyboard("{Control>}a{/Control}");
    expect(selected()).toHaveLength(4);
    await user.keyboard("{Escape}");
    expect(selected()).toEqual([]);
  });

  it("restores the selection and takes it out of the selection", async () => {
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });
    expect(headerButton("Restore")).toBeDisabled();

    await user.click(row(report));
    await user.click(headerButton("Restore"));

    expect(jobs.restore).toHaveBeenCalledWith({ paths: [report.path] }, [
      report,
    ]);
    expect(row(report)).toHaveAttribute("aria-selected", "false");
    expect(headerButton("Restore")).toBeDisabled();
  });

  it("names the destination before restoring an unrecorded item into a system folder", async () => {
    const foreign = entry({
      confirmRestore: true,
      name: "app.conf",
      originalPath: "/etc/app.d/app.conf",
    });
    mocks.entries = [...all, foreign];
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });

    await user.click(row(foreign));
    await user.click(headerButton("Restore"));
    const dialog = await screen.findByRole("dialog", {
      name: "Restore to a system folder",
    });
    expect(dialog).toHaveTextContent("Restore to /etc/app.d/app.conf?");
    expect(jobs.restore).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole("button", { name: "Restore" }));
    expect(jobs.restore).toHaveBeenCalledWith(
      {
        confirmedDestinations: { [foreign.path]: foreign.originalPath },
        paths: [foreign.path],
      },
      [foreign],
    );
  });

  it("restores into a picked folder under the names shown", async () => {
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });

    await user.click(row(notes));
    await user.keyboard("{Control>}");
    await user.click(row(report));
    await user.keyboard("{/Control}");
    await user.click(headerButton("Restore to…"));
    const dialog = await screen.findByRole("dialog", {
      name: "Restore to a folder",
    });
    const field = within(dialog).getByRole("textbox", { name: "Folder" });
    expect(field).toHaveValue("/home/alice");
    await user.clear(field);
    await user.type(field, "/srv/backup/");
    await user.click(within(dialog).getByRole("button", { name: "Restore" }));

    // A restore into a chosen folder confirms nothing: the folder is the
    // user's choice.
    expect(jobs.restore).toHaveBeenCalledWith({
      destination: "/srv/backup",
      names: { [notes.path]: "notes.txt", [report.path]: "report.pdf" },
      paths: [notes.path, report.path],
    });
    expect(row(notes)).toHaveAttribute("aria-selected", "false");
  });

  it("asks before deleting the selection for good, from the Delete key", async () => {
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });

    await user.click(row(photos));
    await user.keyboard("{Delete}");
    const dialog = await screen.findByRole("dialog", {
      name: "Delete permanently",
    });
    expect(dialog).toHaveTextContent(
      "Permanently delete photos? This cannot be undone.",
    );
    const confirm = within(dialog).getByRole("button", { name: "Delete" });
    await user.click(confirm);
    // The confirm fades out with its handler: a second click starts nothing.
    fireEvent.click(confirm);

    expect(jobs.purge).toHaveBeenCalledTimes(1);
    expect(jobs.purge).toHaveBeenCalledWith([photos.path]);
  });

  it("asks before emptying the whole Trash", async () => {
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });

    await user.click(headerButton("Empty Trash"));
    const dialog = await screen.findByRole("dialog", { name: "Empty Trash" });
    expect(dialog).toHaveTextContent(
      "Permanently delete everything in the Trash (4 items)? This cannot be undone.",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Empty Trash" }),
    );

    expect(jobs.purge).toHaveBeenCalledWith([
      notes.path,
      report.path,
      photos.path,
      mystery.path,
    ]);
  });

  it("shows an item's properties when it is opened, and browses nowhere", async () => {
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });

    await user.dblClick(row(photos));
    const dialog = await screen.findByRole("dialog", { name: "Properties" });
    expect(dialog).toHaveTextContent("/srv/media/photos");
    expect(dialog).toHaveTextContent("Folder");
    expect(onLeave).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole("button", { name: "Restore" }));
    expect(jobs.restore).toHaveBeenCalledWith({ paths: [photos.path] }, [
      photos,
    ]);
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Properties" }),
      ).not.toBeInTheDocument(),
    );

    await user.click(row(report));
    await user.keyboard("{Enter}");
    expect(
      await screen.findByRole("dialog", { name: "Properties" }),
    ).toHaveTextContent("/home/alice/docs/report.pdf");
  });

  it("totals the files of several items in their properties", async () => {
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });

    await user.keyboard("{Control>}a{/Control}");
    fireEvent.contextMenu(row(report));
    await user.click(screen.getByRole("menuitem", { name: "Properties" }));

    const dialog = await screen.findByRole("dialog", { name: "Properties" });
    expect(dialog).toHaveTextContent("4 items");
    expect(dialog).toHaveTextContent("3 files, 1 folder");
    expect(dialog).toHaveTextContent("2.02 KB");
  });

  it("goes back to the folders on Backspace", async () => {
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });

    await user.keyboard("{Backspace}");
    expect(onLeave).toHaveBeenCalledTimes(1);
  });

  it("drags items out as a restore carrying their shown names", async () => {
    renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });
    const setData = vi.fn();

    fireEvent.dragStart(row(notes), {
      dataTransfer: { effectAllowed: "", setData },
    });
    expect(setData).toHaveBeenCalledWith(
      TRASH_DRAG_TYPE,
      JSON.stringify([{ name: "notes.txt", path: notes.path }]),
    );
    expect(row(notes)).toHaveAttribute("aria-selected", "true");

    // Dropped where it restores: it leaves the selection.
    fireEvent.dragEnd(row(notes), { dataTransfer: { dropEffect: "move" } });
    expect(row(notes)).toHaveAttribute("aria-selected", "false");
  });

  it("moves files dragged here from a folder to the Trash", async () => {
    renderPlace();
    const list = await screen.findByRole("listbox", { name: "Trash" });
    const dataTransfer = {
      dropEffect: "none",
      getData: (type: string) =>
        type === INTERNAL_DRAG_TYPE ? JSON.stringify(["/srv/x.txt"]) : "",
      types: [INTERNAL_DRAG_TYPE],
    };

    fireEvent.dragOver(list, { dataTransfer });
    fireEvent.drop(list, { dataTransfer });
    expect(onTrashItems).toHaveBeenCalledWith(["/srv/x.txt"]);
  });

  it("says when the Trash is empty", async () => {
    mocks.entries = [];
    renderPlace();

    expect(await screen.findByText("Trash is empty.")).toBeVisible();
    expect(headerButton("Empty Trash")).toBeDisabled();
  });

  it("goes back to the folders on Backspace from an empty Trash", async () => {
    mocks.entries = [];
    const { user } = renderPlace();
    await screen.findByText("Trash is empty.");

    await user.keyboard("{Backspace}");
    expect(onLeave).toHaveBeenCalledTimes(1);
  });

  it("shows a failed list with a retry", async () => {
    mocks.error = new Error("trash unreadable");
    const { user } = renderPlace();

    expect(await screen.findByText("trash unreadable")).toBeVisible();
    mocks.error = null;
    await user.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("listbox", { name: "Trash" })).toBeVisible();
  });
});
