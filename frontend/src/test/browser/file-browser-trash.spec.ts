import { expect, test } from "@playwright/test";

const REPORT = "/home/alice/.local/share/Trash/files/report.pdf";
const PHOTOS = "/home/alice/.local/share/Trash/files/photos";

test.describe("file-browser trash place", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/filebrowser/trash");
    await expect(
      page.getByRole("heading", { name: "Trash place fixture" }),
    ).toBeVisible();
  });

  test("lists entries with their original folders, newest deleted first", async ({
    page,
  }) => {
    const list = page.getByRole("listbox", { name: "Trash" });
    await expect(list.getByRole("option")).toHaveCount(3);
    await expect(list.getByRole("option").first()).toContainText("report.pdf");
    for (const text of ["/home/alice/docs", "/srv/media", "Unknown"]) {
      await expect(list.getByText(text, { exact: true }).first()).toBeVisible();
    }
    await expect(
      page.getByRole("button", { name: "breadcrumb-link-Trash" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Restore", exact: true }),
    ).toBeDisabled();
    await expect(
      page.getByRole("button", { name: "Delete permanently", exact: true }),
    ).toBeDisabled();
  });

  test("selecting a row enables the actions and Restore sends it", async ({
    page,
  }) => {
    const photos = page.getByRole("option", { name: /photos/ });
    await photos.click();
    await expect(photos).toHaveAttribute("aria-selected", "true");

    await expect(
      page.getByRole("button", { name: "Delete permanently", exact: true }),
    ).toBeEnabled();
    await page.getByRole("button", { name: "Restore", exact: true }).click();
    await expect(page.getByTestId("trash-jobs")).toHaveText(
      `restore:${JSON.stringify({ paths: [PHOTOS] })}`,
    );
    // The job's item leaves the selection as it starts.
    await expect(photos).toHaveAttribute("aria-selected", "false");
  });

  test("opening an item shows its properties instead of browsing into it", async ({
    page,
  }) => {
    await page.getByRole("option", { name: /photos/ }).dblclick();

    const dialog = page.getByRole("dialog", { name: "Properties" });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("/srv/media/photos");
    await expect(dialog).toContainText("Folder");
    // A trashed folder is sized when its properties open.
    await expect(dialog).toContainText("4 KB");
    await expect(dialog).toContainText("3 files, 1 folder");
    await expect(page.getByTestId("trash-jobs")).toHaveText("none");
  });

  test("shows the items as cards, as the folder listing does", async ({
    page,
  }) => {
    await page
      .getByRole("button", { name: "Switch to card view" })
      .first()
      .click();

    const list = page.getByRole("listbox", { name: "Trash" });
    await expect(list.getByRole("option")).toHaveCount(3);
    const photos = list.getByRole("option", { name: /photos/ });
    await expect(photos).toContainText("/srv/media");
    await expect(page.getByRole("slider", { name: "Card size" })).toBeVisible();
    await photos.click();
    await expect(photos).toHaveAttribute("aria-selected", "true");
    await expect(
      page.getByRole("button", { name: "Restore", exact: true }),
    ).toBeEnabled();
  });

  test("arrow keys move through the list a row at a time", async ({ page }) => {
    const selected = page
      .getByRole("listbox", { name: "Trash" })
      .locator('[aria-selected="true"]');

    // Nothing is selected yet: the first key starts at the first item.
    await page.keyboard.press("ArrowDown");
    await expect(selected).toHaveCount(1);
    await expect(selected).toContainText("report.pdf");
    await page.keyboard.press("ArrowDown");
    await expect(selected).toContainText("photos");
    // A list has no columns to move across.
    await page.keyboard.press("ArrowRight");
    await expect(selected).toContainText("photos");
    await page.keyboard.press("ArrowUp");
    await expect(selected).toContainText("report.pdf");
  });

  test("arrow keys move through the cards by rows and columns", async ({
    page,
  }) => {
    await page
      .getByRole("button", { name: "Switch to card view" })
      .first()
      .click();
    const selected = page
      .getByRole("listbox", { name: "Trash" })
      .locator('[aria-selected="true"]');

    await page.keyboard.press("ArrowDown");
    await expect(selected).toContainText("report.pdf");
    // Right is the next card in the row; down is a row further, here the
    // last card however many fit a row.
    await page.keyboard.press("ArrowRight");
    await expect(selected).toContainText("photos");
    await page.keyboard.press("ArrowLeft");
    await expect(selected).toContainText("report.pdf");
    await page.keyboard.press("ArrowDown");
    await expect(selected).toContainText("mystery.bin");
  });

  test("Delete asks before deleting for good", async ({ page }) => {
    await page.getByRole("option", { name: /report\.pdf/ }).click();
    await page.keyboard.press("Delete");

    const confirm = page.getByRole("dialog", { name: "Delete permanently" });
    await expect(confirm).toContainText("Permanently delete report.pdf?");
    await confirm.getByRole("button", { name: "Delete" }).click();

    await expect(confirm).toBeHidden();
    await expect(page.getByTestId("trash-jobs")).toHaveText(`purge:${REPORT}`);
  });

  test("Empty Trash asks first and Cancel keeps everything", async ({
    page,
  }) => {
    await page.getByRole("button", { name: "Empty Trash" }).click();

    const confirm = page.getByRole("dialog", {
      name: "Empty Trash",
      exact: true,
    });
    await expect(confirm).toBeVisible();
    await expect(confirm).toContainText("cannot be undone");
    await confirm.getByRole("button", { name: "Cancel" }).click();

    await expect(confirm).toBeHidden();
    await expect(page.getByRole("option")).toHaveCount(3);
    await expect(page.getByTestId("trash-jobs")).toHaveText("none");
  });

  test("a drag this page did not start restores nothing", async ({ page }) => {
    // Playwright drags within one page, so these drops are dispatched as
    // another server's tab, or a page forging this server's, would send them.
    const dropOnAlice = (payload: object) =>
      page
        .getByRole("button", { name: "breadcrumb-link-alice" })
        .evaluate((target, json) => {
          const dataTransfer = new DataTransfer();
          dataTransfer.setData("application/x-linuxio-trash", json);
          for (const type of ["dragover", "drop"]) {
            target.dispatchEvent(
              new DragEvent(type, {
                bubbles: true,
                cancelable: true,
                dataTransfer,
              }),
            );
          }
        }, JSON.stringify(payload));
    const items = [{ name: "report.pdf", path: REPORT }];
    const here = await page.evaluate(() => window.location.origin);

    await dropOnAlice({ items, origin: "https://backup.example:8090" });
    // This server's origin with no token, or a guessed one: forged.
    await dropOnAlice({ items, origin: here });
    await dropOnAlice({ items, origin: here, token: "0".repeat(32) });
    await expect(page.getByTestId("trash-jobs")).toHaveText("none");
  });

  test("dragging an item onto a folder of the trail restores it there", async ({
    page,
  }) => {
    await page
      .getByRole("option", { name: /report\.pdf/ })
      .dragTo(page.getByRole("button", { name: "breadcrumb-link-alice" }));

    await expect(page.getByTestId("trash-jobs")).toHaveText(
      `restoreInto:/home/alice:${JSON.stringify([
        { name: "report.pdf", path: REPORT },
      ])}`,
    );
  });
});
