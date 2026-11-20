import { afterEach, describe, expect, it, vi } from "vitest";

import { fireEvent, render, screen, waitFor, within } from "@/test/render";

import {
  askToDeleteForGood,
  nextNoTrashPrompt,
  settleNoTrashPrompt,
} from "../-hooks/noTrashPrompts";
import NoTrashPrompt from "./NoTrashPrompt";

const promptDialog = () =>
  screen.findByRole("dialog", { name: "Delete permanently" });

describe("NoTrashPrompt", () => {
  afterEach(() => {
    // The questions outlive a page; none may leak into the next test.
    for (let next = nextNoTrashPrompt(); next; next = nextNoTrashPrompt()) {
      settleNoTrashPrompt(next.id);
    }
  });

  it("asks once for a job's items and deletes exactly those", async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    askToDeleteForGood(["/mnt/data/a.txt", "/mnt/data/photos/"]);
    const { user } = render(<NoTrashPrompt onDelete={onDelete} />);

    const dialog = await promptDialog();
    expect(dialog).toHaveTextContent(
      "2 items in /mnt/data cannot go to the Trash. Delete them permanently? This cannot be undone.",
    );
    const confirm = within(dialog).getByRole("button", { name: "Delete" });
    await user.click(confirm);

    expect(onDelete).toHaveBeenCalledTimes(1);
    expect(onDelete).toHaveBeenCalledWith([
      "/mnt/data/a.txt",
      "/mnt/data/photos/",
    ]);
    // The question fades out with its handler: a second click meanwhile
    // deletes nothing more, and it is not asked again.
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Delete permanently" }),
      ).not.toBeInTheDocument(),
    );
    expect(onDelete).toHaveBeenCalledTimes(1);
    expect(nextNoTrashPrompt()).toBeNull();
  });

  it("names a single item and its folder", async () => {
    askToDeleteForGood(["/mnt/usb/report.pdf"]);
    render(<NoTrashPrompt onDelete={vi.fn()} />);

    expect(await promptDialog()).toHaveTextContent(
      "report.pdf in /mnt/usb cannot go to the Trash. Delete it permanently? This cannot be undone.",
    );
  });

  it("deletes nothing when cancelled", async () => {
    const onDelete = vi.fn();
    askToDeleteForGood(["/mnt/usb/report.pdf"]);
    const { user } = render(<NoTrashPrompt onDelete={onDelete} />);

    await user.click(
      within(await promptDialog()).getByRole("button", { name: "Cancel" }),
    );

    expect(onDelete).not.toHaveBeenCalled();
    expect(nextNoTrashPrompt()).toBeNull();
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Delete permanently" }),
      ).not.toBeInTheDocument(),
    );
  });

  it("asks about each of two jobs in turn", async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    askToDeleteForGood(["/mnt/usb/first.txt"]);
    askToDeleteForGood(["/mnt/data/second.txt", "/mnt/data/third.txt"]);
    const { user } = render(<NoTrashPrompt onDelete={onDelete} />);

    const first = await promptDialog();
    expect(first).toHaveTextContent("first.txt in /mnt/usb");
    await user.click(within(first).getByRole("button", { name: "Cancel" }));

    // The second question opens once the first has gone, with its own items.
    await waitFor(() =>
      expect(screen.getByRole("dialog")).toHaveTextContent(
        "2 items in /mnt/data cannot go to the Trash",
      ),
    );
    await user.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Delete",
      }),
    );
    expect(onDelete).toHaveBeenCalledTimes(1);
    expect(onDelete).toHaveBeenCalledWith([
      "/mnt/data/second.txt",
      "/mnt/data/third.txt",
    ]);
  });

  it("still asks after the page that raised it is gone", async () => {
    askToDeleteForGood(["/mnt/usb/report.pdf"]);
    const first = render(<NoTrashPrompt onDelete={vi.fn()} />);
    await promptDialog();
    first.unmount();

    const onDelete = vi.fn().mockResolvedValue(undefined);
    const { user } = render(<NoTrashPrompt onDelete={onDelete} />);
    await user.click(
      within(await promptDialog()).getByRole("button", { name: "Delete" }),
    );
    expect(onDelete).toHaveBeenCalledWith(["/mnt/usb/report.pdf"]);
  });
});
