import { expect, test } from "@playwright/test";

const scrollport =
  '[data-testid="virtual-filebrowser-scrollport"] > .custom-scrollbar';

async function settle(page: import("@playwright/test").Page) {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
}

test.describe("virtual file browser geometry", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/filebrowser/virtual");
    await expect(
      page.getByRole("heading", { name: "Virtual file browser fixture" }),
    ).toBeVisible();
    await expect(page.getByTestId("virtual-filebrowser-status")).toContainText(
      "items: 240",
    );
  });

  test("virtualizes a long list and reaches the final item", async ({
    page,
  }) => {
    const initial = await page.locator(scrollport).evaluate((element) => ({
      clientHeight: element.clientHeight,
      renderedRows: element.querySelectorAll("[data-index]").length,
      scrollHeight: element.scrollHeight,
    }));
    expect(initial.scrollHeight).toBeGreaterThan(initial.clientHeight);
    expect(initial.renderedRows).toBeLessThan(40);

    await page.locator(scrollport).evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await settle(page);
    await expect(page.getByText("fixture-239.txt")).toBeVisible();
  });

  test("preserves geometry through item-count changes", async ({ page }) => {
    await page.locator(scrollport).evaluate((element) => {
      element.scrollTop = 600;
    });
    await settle(page);
    const anchor = page.getByText("fixture-15.txt", { exact: true });
    await expect(anchor).toBeVisible();
    const beforeAnchor = (await anchor.boundingBox())?.y ?? 0;
    const before = await page
      .locator(scrollport)
      .evaluate((element) => element.scrollHeight);
    await page.getByRole("button", { name: "Add items" }).click();
    await expect(page.getByTestId("virtual-filebrowser-status")).toContainText(
      "items: 280",
    );
    await settle(page);
    const afterAdd = await page
      .locator(scrollport)
      .evaluate((element) => element.scrollHeight);
    expect(afterAdd).toBeGreaterThan(before);
    expect(
      Math.abs(((await anchor.boundingBox())?.y ?? 0) - beforeAnchor),
    ).toBeLessThan(2);

    await page.getByRole("button", { name: "Remove items" }).click();
    await expect(page.getByTestId("virtual-filebrowser-status")).toContainText(
      "items: 240",
    );
    await settle(page);
    const afterRemove = await page
      .locator(scrollport)
      .evaluate((element) => element.scrollHeight);
    expect(afterRemove).toBeLessThan(afterAdd);
    expect(
      Math.abs(((await anchor.boundingBox())?.y ?? 0) - beforeAnchor),
    ).toBeLessThan(2);
  });

  test("reflows when switching between list and card layouts", async ({
    page,
  }) => {
    const listRows = await page.locator(`${scrollport} [data-index]`).count();
    await page.getByRole("button", { name: "Card view" }).click();
    await expect(page.getByTestId("virtual-filebrowser-status")).toContainText(
      "view: card",
    );
    await settle(page);
    const cardRows = await page.locator(`${scrollport} [data-index]`).count();
    expect(cardRows).toBeLessThan(listRows);
    await expect(page.getByText("fixture-0.txt")).toBeVisible();
  });

  test("preserves folder and file sections in the lazy row layout", async ({
    page,
  }) => {
    await page.getByRole("button", { name: "Show folders" }).click();
    await expect(page.getByTestId("virtual-filebrowser-status")).toContainText(
      "folders: 5",
    );
    await expect(page.getByRole("heading", { name: "Folders" })).toBeVisible();
    await expect(page.getByText("folder-0", { exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Files" })).toBeVisible();
    await expect(
      page.getByText("fixture-0.txt", { exact: true }),
    ).toBeVisible();

    await page.getByRole("button", { name: "Card view" }).click();
    await settle(page);
    await expect(page.getByText("folder-4", { exact: true })).toBeVisible();
    await expect(
      page.getByText("fixture-0.txt", { exact: true }),
    ).toBeVisible();
  });
});
