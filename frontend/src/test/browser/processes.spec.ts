import { expect, test } from "@playwright/test";

const table = (
  page: import("@playwright/test").Page,
  name: "Processes" | "Programs" = "Processes",
) => page.getByRole("table", { name });

test.describe("processes table", () => {
  test("filters, switches tabs, sorts, and keeps search state shareable", async ({
    page,
  }) => {
    await page.goto("/services/processes");
    await expect(table(page)).toBeVisible();
    await expect(page.getByText("nginx", { exact: true })).toBeVisible();

    await page.getByLabel("Filter processes").fill("queue");
    await expect(page).toHaveURL(/\/services\/processes\?filter=queue$/);
    await expect(page.getByText("worker", { exact: true })).toBeVisible();
    await expect(page.getByText("nginx", { exact: true })).toHaveCount(0);

    await page.getByRole("tab", { name: "Programs" }).click();
    await expect(page).toHaveURL(/\/services\/programs$/);
    await expect(table(page, "Programs")).toHaveAttribute(
      "aria-label",
      "Programs",
    );

    await page
      .getByRole("columnheader")
      .getByRole("button", { name: "CPU" })
      .click();
    const rows = table(page, "Programs").getByRole("row");
    await expect(rows.nth(1)).toContainText("nginx");

    await page.goBack();
    await expect(page).toHaveURL(/\/services\/processes\?filter=queue$/);
    await expect(table(page)).toBeVisible();
  });

  test("replacing a filter does not add a browser history entry", async ({
    page,
  }) => {
    await page.goto("/accounts");
    await page.goto("/services/processes");
    await page.getByLabel("Filter processes").fill("nginx");
    await page.goBack();
    await expect(page).toHaveURL(/\/accounts$/);
  });
});
