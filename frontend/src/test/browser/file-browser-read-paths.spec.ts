import { expect, test } from "@playwright/test";

test.describe("file-browser read paths", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/filebrowser/read-paths");
    await expect(
      page.getByRole("heading", { name: "File-browser read paths fixture" }),
    ).toBeVisible();
  });

  test("expands from the names-only cache", async ({ page }) => {
    const listCache = page.getByTestId("list-directory-cache");
    const beforeExpansion = await listCache.textContent();

    await page.getByRole("button", { name: "Expand /" }).click();
    await expect(page.getByText("folder", { exact: true })).toBeVisible();
    await expect(page.getByText("note.txt", { exact: true })).toBeVisible();
    await expect(page.getByTestId("directory-children-cache")).toContainText(
      "data=cached;status=success;fetch=idle;freshness=fresh",
    );
    await expect(listCache).toHaveText(beforeExpansion ?? "");
  });

  test("invalidates listing without invalidating the editor baseline", async ({
    page,
  }) => {
    await page.getByRole("button", { name: "Invalidate listing" }).click();
    await expect(page.getByTestId("list-directory-cache")).toContainText(
      "freshness=stale",
    );
    await expect(page.getByTestId("read-text-cache")).toContainText(
      "data=cached;status=success;fetch=idle;freshness=fresh",
    );
  });
});
