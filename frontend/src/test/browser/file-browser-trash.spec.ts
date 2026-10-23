import { expect, test } from "@playwright/test";

test.describe("file-browser trash dialog", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/filebrowser/trash");
    await page.getByRole("button", { name: "Open Trash" }).click();
  });

  test("lists entries with their original folders", async ({ page }) => {
    const dialog = page.getByRole("dialog", { name: "Trash (3)" });
    await expect(dialog).toBeVisible();
    for (const text of [
      "report.pdf",
      "/home/alice/docs",
      "photos",
      "/srv/media",
      "mystery.bin",
      "Original location unknown",
    ]) {
      await expect(dialog.getByText(text, { exact: true })).toBeVisible();
    }
    await expect(
      dialog.getByRole("button", { name: "Restore", exact: true }),
    ).toBeDisabled();
    await expect(
      dialog.getByRole("button", {
        name: "Delete permanently",
        exact: true,
      }),
    ).toBeDisabled();
  });

  test("selecting a row enables the actions", async ({ page }) => {
    const dialog = page.getByRole("dialog", { name: "Trash (3)" });
    await dialog.getByText("photos", { exact: true }).click();

    await expect(
      dialog.getByRole("checkbox", { name: "Select photos" }),
    ).toBeChecked();
    await expect(
      dialog.getByRole("button", { name: "Restore 1" }),
    ).toBeEnabled();
    await expect(
      dialog.getByRole("button", { name: "Delete permanently 1" }),
    ).toBeEnabled();
  });

  test("Empty trash asks first and Cancel keeps everything", async ({
    page,
  }) => {
    await page
      .getByRole("dialog", { name: "Trash (3)" })
      .getByRole("button", { name: "Empty trash" })
      .click();

    const confirm = page.getByRole("dialog", {
      name: "Empty trash",
      exact: true,
    });
    await expect(confirm).toBeVisible();
    await expect(confirm).toContainText("cannot be undone");
    await confirm.getByRole("button", { name: "Cancel" }).click();

    await expect(confirm).toBeHidden();
    await expect(page.getByRole("dialog", { name: "Trash (3)" })).toBeVisible();
  });

  test("Escape closes the dialog", async ({ page }) => {
    const dialog = page.getByRole("dialog", { name: "Trash (3)" });
    await expect(dialog).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
  });
});
