import { expect, test } from "@playwright/test";

const grid = '[aria-label="Virtual grid fixture"]';

async function settle(page: import("@playwright/test").Page) {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
}

test.describe("virtual grid geometry", () => {
  test("virtualizes a long list and keeps the end reachable", async ({
    page,
  }) => {
    await page.goto("/grids/virtual");
    await expect(page.getByTestId("virtual-grid-status")).toContainText(
      "items: 180",
    );

    const initial = await page.locator(grid).evaluate((element) => ({
      clientHeight: element.clientHeight,
      rowCount: element.querySelectorAll('[role="row"]').length,
      scrollHeight: element.scrollHeight,
    }));
    expect(initial.scrollHeight).toBeGreaterThan(initial.clientHeight);
    expect(initial.rowCount).toBeLessThan(30);

    await page.locator(grid).evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await settle(page);
    await expect(page.getByText("grid-item-179")).toBeVisible();
  });

  test("updates item-count geometry without losing prepend or append reachability", async ({
    page,
  }) => {
    await page.goto("/grids/virtual");
    const before = await page
      .locator(grid)
      .evaluate((element) => element.scrollHeight);

    await page.getByRole("button", { name: "Append item" }).click();
    await expect(page.getByTestId("virtual-grid-status")).toContainText(
      "items: 181",
    );
    await settle(page);
    const afterAppend = await page
      .locator(grid)
      .evaluate((element) => element.scrollHeight);
    expect(afterAppend).toBeGreaterThan(before);

    await page.getByRole("button", { name: "Prepend item" }).click();
    await expect(page.getByTestId("virtual-grid-status")).toContainText(
      "items: 182",
    );
    await settle(page);
    await page.locator(grid).evaluate((element) => {
      element.scrollTop = 0;
    });
    await expect(page.getByText(/grid-prepend-/)).toBeVisible();
  });

  test("remeasures dynamic rows and reflows when the column count changes", async ({
    page,
  }) => {
    await page.goto("/grids/virtual");
    const firstCard = page.getByTestId("grid-card-0");
    const standardHeight = (await firstCard.boundingBox())?.height ?? 0;
    await page.getByRole("button", { name: "Toggle first height" }).click();
    await expect
      .poll(async () => (await firstCard.boundingBox())?.height)
      .toBeGreaterThan(standardHeight + 50);

    const standardRows = await page.locator(`${grid} [role="row"]`).count();
    await page.getByRole("button", { name: "Toggle columns" }).click();
    await settle(page);
    const wideRows = await page.locator(`${grid} [role="row"]`).count();
    expect(wideRows).toBeGreaterThanOrEqual(standardRows);
    const firstTop =
      (await page.getByTestId("grid-card-0").boundingBox())?.y ?? 0;
    const thirdTop =
      (await page.getByTestId("grid-card-2").boundingBox())?.y ?? 0;
    expect(thirdTop).toBeGreaterThan(firstTop);
  });

  test("keeps scroll geometry stable when a row above the viewport grows", async ({
    page,
  }) => {
    await page.goto("/grids/virtual");
    await page.locator(grid).evaluate((element) => {
      element.scrollTop = 600;
    });
    await settle(page);
    await expect(page.getByTestId("grid-card-0")).toBeAttached();
    const anchor = page.getByTestId("grid-card-54");
    await expect(anchor).toBeVisible();
    const beforeAnchor = (await anchor.boundingBox())?.y ?? 0;
    const before = await page
      .locator(grid)
      .evaluate((element) => element.scrollTop);

    await page.getByRole("button", { name: "Toggle first height" }).click();
    await expect
      .poll(async () =>
        page
          .locator(grid)
          .evaluate(
            (element, initialScrollTop) => element.scrollTop - initialScrollTop,
            before,
          ),
      )
      .toBeGreaterThan(80);
    const afterAnchor = (await anchor.boundingBox())?.y ?? 0;
    const after = await page
      .locator(grid)
      .evaluate((element) => element.scrollTop);
    expect(Math.abs(afterAnchor - beforeAnchor)).toBeLessThan(8);
    expect(after - before).toBeGreaterThan(80);
  });
});
