import { expect, test } from "@playwright/test";

const scrollSelector =
  '[role="table"][aria-label="Virtual expansion table"] > .app-dt__scroll';

async function settle(page: import("@playwright/test").Page) {
  await page.waitForTimeout(260);
}

async function scrollTo(page: import("@playwright/test").Page, top: number) {
  await page.locator(scrollSelector).evaluate((element, value) => {
    (element as HTMLElement).scrollTop = value;
  }, top);
  await page.waitForTimeout(40);
}

async function scrollMetrics(page: import("@playwright/test").Page) {
  return page.locator(scrollSelector).evaluate((element) => ({
    clientHeight: (element as HTMLElement).clientHeight,
    scrollHeight: (element as HTMLElement).scrollHeight,
    scrollTop: (element as HTMLElement).scrollTop,
  }));
}

function row(page: import("@playwright/test").Page, index: number) {
  return page.locator(`#virtual-row-${index}`);
}

test.describe("virtual native table expansion", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/tables/virtual-expansion");
    await expect(
      page.getByRole("heading", { name: "Virtual expansion fixture" }),
    ).toBeVisible();
    await expect(row(page, 0)).toBeVisible();
    await settle(page);
  });

  test("virtualizes the long list and preserves measured detail geometry", async ({
    page,
  }) => {
    const visibleRows = page.locator(
      '[data-testid="virtual-expansion-scrollport"] [id^="virtual-row-"]',
    );
    const count = await visibleRows.count();
    expect(count).toBeGreaterThan(0);
    expect(count).toBeLessThan(40);
    const visibleRowHeights = await visibleRows.evaluateAll((elements) =>
      elements.map((element) => element.getBoundingClientRect().height),
    );
    expect(visibleRowHeights.length).toBeGreaterThan(0);
    expect(Math.min(...visibleRowHeights)).toBeGreaterThanOrEqual(48);

    const before = await scrollMetrics(page);
    expect(before.scrollHeight).toBeGreaterThan(before.clientHeight * 8);

    await row(page, 0).click();
    await expect(page.getByTestId("detail-virtual-row-0")).toBeVisible();
    await settle(page);
    const expanded = await scrollMetrics(page);
    expect(expanded.scrollHeight - before.scrollHeight).toBeGreaterThan(40);

    await row(page, 0).click();
    await expect(page.getByTestId("detail-virtual-row-0")).toBeHidden();
    await settle(page);
    const collapsed = await scrollMetrics(page);
    expect(Math.abs(collapsed.scrollHeight - before.scrollHeight)).toBeLessThan(
      8,
    );
  });

  test("keeps multiple details independent, updates dynamic growth, and reaches nested rows", async ({
    page,
  }) => {
    await row(page, 0).click();
    await row(page, 1).click();
    await expect(page.getByText("Nested detail A")).toBeVisible();
    await expect(page.getByTestId("detail-virtual-row-1")).toBeVisible();
    await expect(page.getByTestId("detail-virtual-row-0")).toBeVisible();

    const beforeGrowth = await scrollMetrics(page);
    await page.getByRole("button", { name: "Grow detail" }).nth(0).click();
    await settle(page);
    const afterGrowth = await scrollMetrics(page);
    expect(
      afterGrowth.scrollHeight - beforeGrowth.scrollHeight,
    ).toBeGreaterThan(70);

    // Clicking the first row closes only its own detail; the second remains.
    await row(page, 0).click();
    await expect(page.getByTestId("detail-virtual-row-0")).toBeHidden();
    await expect(page.getByTestId("detail-virtual-row-1")).toBeVisible();
  });

  test("anchors a visible row when Escape collapses an above-viewport detail", async ({
    page,
  }) => {
    await row(page, 0).click();
    await expect(page.getByTestId("detail-virtual-row-0")).toBeVisible();
    await settle(page);
    await scrollTo(page, 520);

    const anchor = row(page, 14);
    await expect(anchor).toBeVisible();
    const beforeTop = await anchor.evaluate((element) =>
      Math.round(element.getBoundingClientRect().top),
    );

    await page.keyboard.press("Escape");
    await expect(page.getByTestId("detail-virtual-row-0")).toBeHidden();
    await settle(page);
    const afterTop = await anchor.evaluate((element) =>
      Math.round(element.getBoundingClientRect().top),
    );
    expect(Math.abs(afterTop - beforeTop)).toBeLessThan(8);
  });

  test("settles after rapid expand, collapse, and reopen", async ({ page }) => {
    const target = row(page, 2);
    await target.click();
    await target.click();
    await target.click();
    await expect(page.getByTestId("detail-virtual-row-2")).toBeVisible();
    await settle(page);
    const metrics = await scrollMetrics(page);
    expect(metrics.scrollHeight).toBeGreaterThan(metrics.clientHeight * 8);
  });
});
