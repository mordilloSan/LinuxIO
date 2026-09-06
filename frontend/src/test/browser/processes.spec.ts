import { expect, test } from "@playwright/test";

const table = (
  page: import("@playwright/test").Page,
  name: "Processes" | "Programs" = "Processes",
) => page.getByRole("table", { name });

test.describe("processes table", () => {
  test("filters, switches views, sorts, and keeps search state shareable", async ({
    page,
  }) => {
    await page.goto("/processes");
    await expect(
      page.getByRole("heading", { name: "Processes" }),
    ).toBeVisible();
    await expect(page.getByText("nginx", { exact: true })).toBeVisible();

    await page.getByLabel("Filter processes").fill("queue");
    await expect(page).toHaveURL(/\/processes\?filter=queue$/);
    await expect(page.getByText("worker", { exact: true })).toBeVisible();
    await expect(page.getByText("nginx", { exact: true })).toHaveCount(0);

    await page.getByRole("tab", { name: "Programs" }).click();
    await expect(page).toHaveURL(/\/processes\?filter=queue&view=programs$/);
    await expect(page.getByText(/2 total/)).toBeVisible();
    await expect(table(page, "Programs")).toHaveAttribute(
      "aria-label",
      "Programs",
    );

    await page.getByLabel("Filter processes").fill("");
    await page
      .getByRole("columnheader")
      .getByRole("button", { name: "CPU" })
      .click();
    const rows = table(page, "Programs").getByRole("row");
    await expect(rows.nth(1)).toContainText("nginx");

    await page.goBack();
    await expect(page).toHaveURL(/\/processes\?filter=queue$/);
    await expect(
      page.getByRole("heading", { name: "Processes" }),
    ).toBeVisible();
  });

  test("replacing a filter does not add a browser history entry", async ({
    page,
  }) => {
    await page.goto("/accounts");
    await page.goto("/processes");
    await page.getByLabel("Filter processes").fill("nginx");
    await page.goBack();
    await expect(page).toHaveURL(/\/accounts$/);
  });
});
