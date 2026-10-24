import { expect, test, type Page } from "@playwright/test";

const row = (page: Page, name: string) =>
  page.locator("[data-file-card='true']", { hasText: name });

test.describe("file-browser listing interactions", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/filebrowser/delete-flows");
    await expect(row(page, "a.txt")).toBeVisible();
    await expect(row(page, "photos")).toBeVisible();
  });

  test("dragging a row onto a folder moves it", async ({ page }) => {
    await row(page, "a.txt").dragTo(row(page, "photos"));

    await expect(page.getByTestId("drop-result")).toHaveText(
      "move:/fixture/a.txt->/fixture/photos/",
    );
  });

  test("dragging with Control held copies", async ({ page }) => {
    await page.keyboard.down("Control");
    await row(page, "a.txt").dragTo(row(page, "photos"));
    await page.keyboard.up("Control");

    await expect(page.getByTestId("drop-result")).toHaveText(
      "copy:/fixture/a.txt->/fixture/photos/",
    );
  });

  test("ArrowDown twice selects the second item", async ({ page }) => {
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("ArrowDown");

    // The listing is photos, a.txt, b.txt; Delete acts on the selection.
    await page.keyboard.press("Delete");
    await expect(page.getByRole("dialog")).toContainText(
      "Move a.txt to Trash?",
    );
    await page.keyboard.press("Enter");
    await expect(page.getByTestId("delete-result")).toHaveText(
      "trash:/fixture/a.txt",
    );
  });

  test("Enter opens the focused folder", async ({ page }) => {
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("Enter");

    await expect(page.getByTestId("open-result")).toHaveText(
      "/fixture/photos/",
    );
  });

  test("Backspace goes to the parent folder", async ({ page }) => {
    await page.keyboard.press("Backspace");

    await expect(page.getByTestId("up-result")).toHaveText("up");
  });

  test("typing a prefix quickly selects the matching folder", async ({
    page,
  }) => {
    await page.keyboard.type("ph");
    await page.keyboard.press("Enter");

    await expect(page.getByTestId("open-result")).toHaveText(
      "/fixture/photos/",
    );
  });
});
