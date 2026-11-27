import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { TrashEntry } from "@/api";
import { createQueryClient } from "@/api/query-client";
import { dragMadeHere } from "@/test/fileBrowserDrag";
import { fireEvent, render, screen, waitFor, within } from "@/test/render";

import { INTERNAL_DRAG_TYPE, TRASH_DRAG_TYPE } from "../-dragTypes";
import type { TrashJobs } from "../-hooks/useTrashJobs";
import { setInternalDrag } from "../-internalDrag";
import { DEFAULT_TRASH_SORT, type TrashSort } from "../-trashEntries";
import type { ViewMode } from "../-types";
import TrashPlace from "./TrashPlace";

// The Trash shows the folder listing's virtual rows; jsdom has no layout, so
// every row mounts.
vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

vi.mock("@/hooks/useGridColumnCount", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useGridColumnCount")>()),
  useGridColumnCount: () => 2,
}));

const mocks = vi.hoisted(() => ({
  entries: [] as unknown[],
  error: null as Error | null,
  folderSizes: [] as { path: string; size: number }[],
  indexReads: [] as string[],
  sizeError: null as Error | null,
  sized: [] as string[],
  usage: { partial: false, pending: false, size: 0 },
  toastError: vi.fn(),
}));

vi.mock("sonner", async (importOriginal) => ({
  ...(await importOriginal<typeof import("sonner")>()),
  toast: { error: mocks.toastError },
}));

// The app's query client runs queries only while the transport is up.
vi.mock("@/api/linuxio", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/linuxio")>()),
  isRequestAvailable: () => true,
  subscribeRequestAvailability: () => () => {},
}));

// Items dragged from a folder: by this page's own drag, or from another
// server's tab, which has a token of its own.
const carrying = (paths: string[], origin?: string) => {
  const payload = origin
    ? { origin, paths, token: "f".repeat(32) }
    : dragMadeHere((started) => setInternalDrag(started, paths));
  return {
    dropEffect: "none",
    getData: (type: string) =>
      type === INTERNAL_DRAG_TYPE ? JSON.stringify(payload) : "",
    types: [INTERNAL_DRAG_TYPE],
  };
};

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
        subfolders: ({ path }: { path: string }) => ({
          queryFn: async () => {
            mocks.indexReads.push(`subfolders ${path}`);
            return { pending: false, subfolders: mocks.folderSizes };
          },
          queryKey: ["linuxio", "filebrowser", "subfolders", { path }],
        }),
        trash_usage: {
          queryFn: async () => {
            mocks.indexReads.push("trash_usage");
            return mocks.usage;
          },
          queryKey: ["linuxio", "filebrowser", "trash_usage"],
        },
        trash_size: ({ path }: { path: string }) => ({
          queryFn: async () => {
            mocks.sized.push(path);
            if (mocks.sizeError) throw mocks.sizeError;
            return { fileCount: 12, folderCount: 1, size: 1536 };
          },
          queryKey: ["linuxio", "filebrowser", "trash_size", { path }],
        }),
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
const onSortChange = vi.fn();
const onSwitchView = vi.fn();
const onCardSizeChange = vi.fn();
// jsdom lays nothing out: the area the list fills is this wide.
let areaWidth = 1200;

// The page keeps the sort in the URL and the view where the folder listing
// keeps it; here both are the harness's state.
const Place = ({
  initialSort,
  initialView,
}: {
  initialSort: TrashSort;
  initialView: ViewMode;
}) => {
  const [sort, setSort] = useState(initialSort);
  const [viewMode, setViewMode] = useState(initialView);
  const [cardSize, setCardSize] = useState(3);
  return (
    <TrashPlace
      breadcrumbs={<div>Trail</div>}
      cardSize={cardSize}
      folderPath="/home/alice"
      jobs={jobs}
      onCardSizeChange={(next) => {
        onCardSizeChange(next);
        setCardSize(next);
      }}
      onLeave={onLeave}
      onSortChange={(next) => {
        onSortChange(next);
        setSort(next);
      }}
      onSwitchView={() => {
        onSwitchView();
        setViewMode((previous) => (previous === "card" ? "list" : "card"));
      }}
      onTrashItems={onTrashItems}
      sort={sort}
      viewMode={viewMode}
    />
  );
};

const renderPlace = (
  initialSort: TrashSort = DEFAULT_TRASH_SORT,
  initialView: ViewMode = "list",
  privileged = false,
) =>
  render(<Place initialSort={initialSort} initialView={initialView} />, {
    auth: { privileged },
  });

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
    mocks.folderSizes = [];
    mocks.indexReads = [];
    mocks.sizeError = null;
    mocks.sized = [];
    mocks.usage = { partial: false, pending: false, size: 0 };
    areaWidth = 1200;
    const width = vi
      .spyOn(HTMLElement.prototype, "clientWidth", "get")
      .mockImplementation(() => areaWidth);
    return () => width.mockRestore();
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

  it("lists a trashed folder's size from the index and sorts by it", async () => {
    mocks.folderSizes = [{ path: photos.path, size: 36_507_222_016 }];
    const { user } = renderPlace(DEFAULT_TRASH_SORT, "list", true);

    await screen.findByRole("listbox", { name: "Trash" });
    expect(await within(row(photos)).findByText("34 GB")).toBeVisible();
    // One read sizes every folder of a trash.
    expect(
      mocks.indexReads.filter((read) => read.startsWith("subfolders")),
    ).toEqual([`subfolders ${TRASH}`]);

    // Ascending first: the folder is the largest, and so the last.
    await user.click(screen.getByRole("button", { name: "Sort by Size" }));
    expect(rowOrder().at(-1)).toBe(photos.path);
  });

  it("shows a trashed folder's size on its card", async () => {
    mocks.folderSizes = [{ path: photos.path, size: 36_507_222_016 }];
    renderPlace(DEFAULT_TRASH_SORT, "card", true);

    await screen.findByRole("listbox", { name: "Trash" });
    expect(await within(row(photos)).findByText(/34 GB/)).toBeVisible();
  });

  it("says how many items the Trash holds and what they take on disk", async () => {
    mocks.usage = { partial: false, pending: false, size: 36_507_222_016 };
    renderPlace(DEFAULT_TRASH_SORT, "list", true);

    expect(await screen.findByText("4 items, 34 GB")).toBeVisible();
  });

  it("says when what the Trash takes is a lower bound", async () => {
    mocks.usage = { partial: true, pending: false, size: 2048 };
    renderPlace(DEFAULT_TRASH_SORT, "list", true);

    expect(await screen.findByText("4 items, at least 2 KB")).toBeVisible();
  });

  it("asks the index for nothing on a session without sudo", async () => {
    mocks.usage = { partial: false, pending: false, size: 36_507_222_016 };
    renderPlace();

    expect(await screen.findByText("4 items")).toBeVisible();
    expect(mocks.indexReads).toEqual([]);
  });

  it("moves by the column count in card view and a row at a time in the list", async () => {
    const selected = () =>
      screen
        .getAllByRole("option")
        .filter((option) => option.getAttribute("aria-selected") === "true")
        .map((option) => option.getAttribute("data-file-path"));
    const { user } = renderPlace(DEFAULT_TRASH_SORT, "card");
    await screen.findByRole("listbox", { name: "Trash" });

    // Two cards fit a row here.
    await user.keyboard("{ArrowDown}");
    expect(selected()).toEqual([notes.path]);
    await user.keyboard("{ArrowDown}");
    expect(selected()).toEqual([photos.path]);
    await user.keyboard("{ArrowRight}");
    expect(selected()).toEqual([mystery.path]);
    await user.keyboard("{ArrowUp}");
    expect(selected()).toEqual([report.path]);

    await user.click(headerButton("Switch to list view"));
    await user.keyboard("{ArrowDown}");
    expect(selected()).toEqual([photos.path]);
  });

  it("is a listbox of options alone, each placed in the whole Trash", async () => {
    renderPlace(DEFAULT_TRASH_SORT, "card");
    const list = await screen.findByRole("listbox", { name: "Trash" });

    expect(within(list).queryByRole("heading")).not.toBeInTheDocument();
    expect(within(list).queryByRole("slider")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Items" })).toBeVisible();
    expect(
      within(list)
        .getAllByRole("option")
        .map((option) => [
          option.getAttribute("aria-posinset"),
          option.getAttribute("aria-setsize"),
        ]),
    ).toEqual([
      ["1", "4"],
      ["2", "4"],
      ["3", "4"],
      ["4", "4"],
    ]);
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
    // Each choice goes to the page, which keeps it in the URL.
    expect(onSortChange).toHaveBeenLastCalledWith({
      field: "size",
      order: "asc",
    });
  });

  it("opens in the sort the page gives it", async () => {
    renderPlace({ field: "name", order: "desc" });

    await screen.findByRole("listbox", { name: "Trash" });
    expect(rowOrder()).toEqual([
      report.path,
      photos.path,
      notes.path,
      mystery.path,
    ]);
    expect(
      screen.getByRole("button", { name: "Sort by Name, descending" }),
    ).toBeVisible();
  });

  it("shows the items as cards in the listing's card view", async () => {
    const { user } = renderPlace(DEFAULT_TRASH_SORT, "card");

    await screen.findByRole("listbox", { name: "Trash" });
    expect(rowOrder()).toEqual([
      notes.path,
      report.path,
      photos.path,
      mystery.path,
    ]);
    expect(row(notes)).toHaveClass("file-card");
    // A card names the item, where it came from and its size.
    expect(within(row(notes)).getByText("notes.txt")).toBeVisible();
    expect(within(row(notes)).getByText("/home/alice")).toBeVisible();
    expect(within(row(report)).getByText("2 KB")).toBeVisible();
    expect(within(row(mystery)).getByText("Unknown")).toBeVisible();

    await user.click(row(photos));
    expect(row(photos)).toHaveAttribute("aria-selected", "true");
    await user.dblClick(row(photos));
    expect(
      await screen.findByRole("dialog", { name: "Properties" }),
    ).toHaveTextContent("/srv/media/photos");
  });

  it("puts the card-size slider beside the view button, in card view only", async () => {
    const { user } = renderPlace(DEFAULT_TRASH_SORT, "card");
    const list = await screen.findByRole("listbox", { name: "Trash" });

    const slider = screen.getByRole("slider", { name: "Card size" });
    expect(slider).toHaveValue("3");
    expect(list).not.toContainElement(slider);
    const actions = document.querySelector(".file-browser-header__actions");
    expect(actions).toContainElement(slider);
    expect(
      slider.compareDocumentPosition(headerButton("Switch to list view")) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    fireEvent.change(slider, { target: { value: "5" } });
    expect(onCardSizeChange).toHaveBeenCalledWith(5);
    expect(slider).toHaveValue("5");

    await user.click(headerButton("Switch to list view"));
    expect(screen.queryByRole("slider")).not.toBeInTheDocument();
  });

  it("hides the card-size slider where one card fills a row", async () => {
    areaWidth = 379;
    renderPlace(DEFAULT_TRASH_SORT, "card");
    await screen.findByRole("listbox", { name: "Trash" });

    expect(screen.queryByRole("slider")).not.toBeInTheDocument();
  });

  it("keeps the card-size slider in an empty Trash", async () => {
    mocks.entries = [];
    renderPlace(DEFAULT_TRASH_SORT, "card");
    await screen.findByText("Trash is empty.");

    expect(screen.getByRole("slider", { name: "Card size" })).toBeVisible();
  });

  it("switches between cards and the list as the folder listing does", async () => {
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });
    expect(row(notes)).toHaveClass("file-row");

    await user.click(headerButton("Switch to card view"));
    expect(onSwitchView).toHaveBeenCalledTimes(1);
    expect(row(notes)).toHaveClass("file-card");
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

  it("sizes a trashed folder when its properties open, and only then", async () => {
    const { user } = renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });
    // The list keeps no size for a folder.
    expect(mocks.sized).toEqual([]);

    await user.dblClick(row(photos));
    const dialog = await screen.findByRole("dialog", { name: "Properties" });
    await waitFor(() => expect(dialog).toHaveTextContent("1.5 KB"));
    expect(dialog).toHaveTextContent("12 files, 1 folder");
    expect(mocks.sized).toEqual([photos.path]);
  });

  it("says when a trashed folder's size cannot be read, once and only there", async () => {
    mocks.sizeError = new Error("a link was planted");
    // The app's own cache, which retries a failed read and toasts its error
    // unless the query says otherwise.
    const queryClient = createQueryClient();
    const { user } = render(
      <Place initialSort={DEFAULT_TRASH_SORT} initialView="list" />,
      {
        queryClient,
      },
    );
    await screen.findByRole("listbox", { name: "Trash" });

    await user.dblClick(row(photos));
    const dialog = await screen.findByRole("dialog", { name: "Properties" });
    await waitFor(() => expect(dialog).toHaveTextContent("Unavailable"));
    // Size and contents both say why.
    expect(within(dialog).getAllByTitle("a link was planted")).toHaveLength(2);
    // A walk is not repeated on a failure, and the dialog alone says so.
    const query = queryClient.getQueryCache().find({
      queryKey: ["linuxio", "filebrowser", "trash_size", { path: photos.path }],
    });
    expect(query?.options.retry).toBe(false);
    expect(query?.meta?.silent).toBe(true);
    expect(mocks.sized).toEqual([photos.path]);
    expect(mocks.toastError).not.toHaveBeenCalled();
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

  it("drags items out as a restore carrying their shown names and server", async () => {
    renderPlace();
    await screen.findByRole("listbox", { name: "Trash" });
    const setData = vi.fn();

    fireEvent.dragStart(row(notes), {
      dataTransfer: { effectAllowed: "", setData },
    });
    // The page it came from and the drag's token, so a drop elsewhere can
    // tell this drag from one another page forged.
    expect(setData).toHaveBeenCalledWith(TRASH_DRAG_TYPE, expect.any(String));
    expect(JSON.parse(setData.mock.calls[0][1] as string)).toEqual({
      items: [{ name: "notes.txt", path: notes.path }],
      origin: window.location.origin,
      token: expect.stringMatching(/^[0-9a-f]{32}$/),
    });
    expect(row(notes)).toHaveAttribute("aria-selected", "true");

    // Dropped where it restores: it leaves the selection.
    fireEvent.dragEnd(row(notes), { dataTransfer: { dropEffect: "move" } });
    expect(row(notes)).toHaveAttribute("aria-selected", "false");
  });

  it("moves files dragged here from a folder to the Trash", async () => {
    renderPlace();
    const list = await screen.findByRole("listbox", { name: "Trash" });
    const dataTransfer = carrying(["/srv/x.txt"]);

    fireEvent.dragOver(list, { dataTransfer });
    fireEvent.drop(list, { dataTransfer });
    expect(onTrashItems).toHaveBeenCalledWith(["/srv/x.txt"]);
  });

  it("keeps the browser from opening desktop files dropped on the list", async () => {
    renderPlace();
    const list = await screen.findByRole("listbox", { name: "Trash" });
    const files = { dropEffect: "copy", getData: () => "", types: ["Files"] };

    // fireEvent answers false when the handler prevented the default.
    expect(fireEvent.dragOver(list, { dataTransfer: files })).toBe(false);
    expect(files.dropEffect).toBe("none");
    expect(fireEvent.drop(list, { dataTransfer: files })).toBe(false);
    expect(onTrashItems).not.toHaveBeenCalled();
  });

  it("refuses files dragged here from another server's tab", async () => {
    renderPlace();
    const list = await screen.findByRole("listbox", { name: "Trash" });
    const other = "https://backup.example:8090";

    fireEvent.drop(list, { dataTransfer: carrying(["/srv/x.txt"], other) });

    expect(onTrashItems).not.toHaveBeenCalled();
    expect(mocks.toastError).toHaveBeenCalledWith("Cannot drop 1 item here", {
      description: `It comes from ${other}, another server. Drop it in a tab of that server.`,
    });
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
