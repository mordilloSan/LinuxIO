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
    await expect(page.getByTestId("trash-jobs")).toHaveText("none");
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
