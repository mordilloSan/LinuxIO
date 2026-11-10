import { expect, test } from "@playwright/test";

test("fills the page with history rows instead of collapsing to no height", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/history");

  const table = page.getByRole("table", { name: "Job history" });
  await expect(table).toBeVisible();
  const box = await table.boundingBox();
  expect(box?.height ?? 0).toBeGreaterThan(100);
  // A row names what the job did to its label.
  await expect(
    page.getByText("Compressing Archive 0", { exact: true }),
  ).toBeVisible();
});
