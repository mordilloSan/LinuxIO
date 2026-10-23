import { expect, test } from "@playwright/test";

test.describe("file-browser delete flows", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/filebrowser/delete-flows");
    await expect(
      page.getByRole("heading", { name: "File-browser delete flows fixture" }),
    ).toBeVisible();
    await expect(page.getByText("a.txt", { exact: true })).toBeVisible();
  });

  test("Delete asks to move to Trash and Enter confirms", async ({ page }) => {
    await page.getByText("a.txt", { exact: true }).click();
    await page.keyboard.press("Delete");

    const dialog = page.getByRole("dialog", { name: "Move to Trash" });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("Move a.txt to Trash?");
    await page.keyboard.press("Enter");

    await expect(dialog).toBeHidden();
    await expect(page.getByTestId("delete-result")).toHaveText(
      "trash:/fixture/a.txt",
    );
  });

  test("Shift+Delete asks for a permanent delete", async ({ page }) => {
    await page.getByText("a.txt", { exact: true }).click();
    await page.keyboard.press("Shift+Delete");

    const dialog = page.getByRole("dialog", { name: "Delete permanently" });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("This cannot be undone.");
    await dialog.getByRole("button", { name: "Cancel" }).click();

    await expect(dialog).toBeHidden();
    await expect(page.getByTestId("delete-result")).toHaveText("none");
  });

  test("the context menu offers Delete and Delete permanently", async ({
    page,
  }) => {
    await page.getByText("b.txt", { exact: true }).click({ button: "right" });

    const menu = page.getByRole("menu");
    await expect(
      // Menu items carry their shortcut hint ("Delete Del").
      menu.getByRole("menuitem", { name: /^Delete(?! permanently)/ }),
    ).toBeEnabled();
    await menu.getByRole("menuitem", { name: "Delete permanently" }).click();

    const dialog = page.getByRole("dialog", { name: "Delete permanently" });
    await expect(dialog).toContainText("Permanently delete b.txt?");
    await dialog.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(page.getByTestId("delete-result")).toHaveText(
      "permanent:/fixture/b.txt",
    );
  });

  test("the conflict dialog merges folders and overwrites files", async ({
    page,
  }) => {
    await page.getByRole("button", { name: "Open conflicts" }).click();
    const dialog = page.getByRole("dialog");

    const folderRow = dialog.locator("li", { hasText: "photos/" });
    await expect(
      folderRow.getByRole("button", { name: "Merge" }),
    ).toBeVisible();
    const fileRow = dialog.locator("li", { hasText: "a.txt" });
    await expect(
      fileRow.getByRole("button", { name: "Overwrite" }),
    ).toBeVisible();

    await dialog.getByRole("button", { name: "Overwrite / merge all" }).click();
    await dialog.getByRole("button", { name: "Continue" }).click();
    await expect(page.getByTestId("conflict-result")).toHaveText(
      '{"/dst/photos":"overwrite","/dst/a.txt":"overwrite"}',
    );
  });
});
