import { expect, test } from "@playwright/test";

test.describe("file editor history", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/filebrowser/editor-history");
    await expect(
      page.getByRole("heading", { name: "File editor history fixture" }),
    ).toBeVisible();
  });

  test("Back leaves a clean editor", async ({ page }) => {
    const editing = page.getByTestId("editing-path");

    await page.getByRole("button", { name: "Open note" }).click();
    await expect(editing).toHaveText("/note.txt");
    await expect(page).toHaveURL(/[?&]edit=%2Fnote\.txt/);

    await page.goBack();
    await expect(editing).toHaveText("none");
    await expect(page).not.toHaveURL(/edit=/);
  });

  test("Back stops on unsaved changes until discarded", async ({ page }) => {
    const editing = page.getByTestId("editing-path");
    const prompt = page.getByRole("alertdialog", { name: "Unsaved changes" });

    await page.getByRole("button", { name: "Open note" }).click();
    await expect(editing).toHaveText("/note.txt");
    await page.getByRole("button", { name: "Make dirty" }).click();

    await page.goBack();
    await expect(prompt).toBeVisible();
    await page.getByRole("button", { name: "Keep editing" }).click();
    await expect(prompt).toBeHidden();
    await expect(editing).toHaveText("/note.txt");
    await expect(page).toHaveURL(/[?&]edit=%2Fnote\.txt/);

    await page.goBack();
    await expect(prompt).toBeVisible();
    await page.getByRole("button", { name: "Discard and exit" }).click();
    await expect(editing).toHaveText("none");
    await expect(page).not.toHaveURL(/edit=/);
  });

  test("the close button steps back without a duplicate entry", async ({
    page,
  }) => {
    const editing = page.getByTestId("editing-path");

    await page.getByRole("button", { name: "Open note" }).click();
    await expect(editing).toHaveText("/note.txt");
    await page.getByRole("button", { name: "Close editor" }).click();
    await expect(editing).toHaveText("none");

    await page.goForward();
    await expect(editing).toHaveText("/note.txt");
  });
});
