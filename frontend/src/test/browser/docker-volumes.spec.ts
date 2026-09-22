import { expect, test } from "@playwright/test";

test("keeps volume usage and paths readable in tables and cards", async ({
  page,
}, testInfo) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/docker/volumes");
  const table = page.getByRole("table", { name: "Docker volumes" });
  const path = "/var/lib/docker/volumes/immich_model-cache/_data";
  for (const width of [1280, 640, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(
      table.getByRole("columnheader", { name: "Size", exact: true }),
    ).toBeVisible();
    await expect(
      table.getByRole("columnheader", { name: "References" }),
    ).toBeVisible();
    await expect(table.getByText("2 GB", { exact: true })).toBeVisible();
    await expect(table.getByText("0 Bytes", { exact: true })).toBeVisible();
    await expect(table.getByText(path, { exact: true })).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    const bounds = await table
      .getByRole("columnheader", { name: "References" })
      .boundingBox();
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
    await page.screenshot({
      path: testInfo.outputPath(`volumes-table-${width}.png`),
    });
  }

  await page.getByRole("button", { name: "Actions", exact: true }).click();
  await page.getByRole("button", { name: "Switch to card view" }).click();
  await page.keyboard.press("Escape");
  const card = page.getByLabel("Open volume immich_model-cache details", {
    exact: true,
  });
  for (const width of [320, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(card.getByText("Size", { exact: true })).toBeVisible();
    await expect(card.getByText("2 GB", { exact: true })).toBeVisible();
    await expect(card.getByText("References", { exact: true })).toBeVisible();
    await expect(card.getByText("2", { exact: true })).toBeVisible();
    await expect(card.getByText(path, { exact: true })).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`volumes-cards-${width}.png`),
    });
  }
  await card.focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/volume=immich_model-cache/);
  await expect(
    page.getByRole("button", { name: "Close volume details" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Close volume details" }).click();
  const sortable = page.locator(".sortable-card").first();
  await sortable.focus();
  await page.keyboard.press("Space");
  await expect(sortable).toHaveAttribute("aria-pressed", "true");
  await page.keyboard.press("Escape");
  await expect(sortable).not.toHaveAttribute("aria-pressed", "true");
  await page.getByPlaceholder("Search volumes…").fill("no-such-volume");
  await expect(page.getByText("No volumes found.")).toBeVisible();
});
