import { expect, test } from "@playwright/test";

const table = (page: import("@playwright/test").Page) =>
  page.getByRole("table", { name: "Programs" });

test.describe("programs table", () => {
  test("filters, sorts, and keeps search state shareable", async ({ page }) => {
    await page.goto("/services/programs?filter=work");
    await expect(table(page)).toBeVisible();
    await expect(page.getByText("worker", { exact: true })).toBeVisible();
    await expect(page.getByText("nginx", { exact: true })).toHaveCount(0);

    await page.getByLabel("Filter programs").fill("");
    await expect(page).toHaveURL(/\/services\/programs$/);
    await expect(page.getByText("nginx", { exact: true })).toBeVisible();

    await page
      .getByRole("columnheader")
      .getByRole("button", { name: "CPU" })
      .click();
    const rows = table(page).getByRole("row");
    await expect(rows.nth(1)).toContainText("nginx");

    await page.getByLabel("Filter programs").fill("nginx");
    await expect(page).toHaveURL(/\/services\/programs\?filter=nginx$/);
    await expect(page.getByText("worker", { exact: true })).toHaveCount(0);
  });

  test("replacing a filter does not add a browser history entry", async ({
    page,
  }) => {
    await page.goto("/accounts");
    await page.goto("/services/programs");
    await page.getByLabel("Filter programs").fill("nginx");
    await page.goBack();
    await expect(page).toHaveURL(/\/accounts$/);
  });
});
