import { expect, test } from "@playwright/test";

const scrollport = '[data-testid="virtual-filebrowser-scrollport"] > div';

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

  test("uses the global app scrollbar activity states", async ({ page }) => {
    const scroller = page.locator(scrollport);
    const scrollbarStyle = () =>
      scroller.evaluate((element) => {
        const style = getComputedStyle(element, "::-webkit-scrollbar-thumb");
        return {
          color: style.backgroundColor,
          stateColor: getComputedStyle(element)
            .getPropertyValue("--app-scrollbar-thumb-current")
            .trim(),
          width: getComputedStyle(
            element,
            "::-webkit-scrollbar",
          ).getPropertyValue("width"),
        };
      });

    await expect.poll(scrollbarStyle).toEqual({
      color: "rgba(127, 127, 127, 0.06)",
      stateColor: "#7f7f7f0f",
      width: "8px",
    });

    await scroller.hover();
    await expect
      .poll(async () => (await scrollbarStyle()).stateColor)
      .toBe("#64646433");

    const box = await scroller.boundingBox();
    expect(box).not.toBeNull();
    await page.mouse.move(box!.x + box!.width - 4, box!.y + 8);
    await expect.poll(scrollbarStyle).toEqual({
      color: "rgba(100, 100, 100, 0.45)",
      stateColor: "#64646433",
      width: "8px",
    });

    await page
      .getByRole("heading", { name: "Virtual file browser fixture" })
      .hover();
    await expect.poll(scrollbarStyle).toEqual({
      color: "rgba(127, 127, 127, 0.06)",
      stateColor: "#7f7f7f0f",
      width: "8px",
    });

    await scroller.dispatchEvent("scroll");
    await expect(scroller).toHaveAttribute("data-app-scrolling", "");
    await expect.poll(scrollbarStyle).toEqual({
      color: "rgba(100, 100, 100, 0.2)",
      stateColor: "#64646433",
      width: "8px",
    });

    await scroller.dispatchEvent("scrollend");
    await expect(scroller).not.toHaveAttribute("data-app-scrolling");
    await expect.poll(scrollbarStyle).toEqual({
      color: "rgba(127, 127, 127, 0.06)",
      stateColor: "#7f7f7f0f",
      width: "8px",
    });

    await scroller.evaluate((element) => {
      element.setAttribute("data-scroll-activity-log", "");
      const observer = new MutationObserver(() => {
        const state = element.hasAttribute("data-app-scrolling")
          ? "active"
          : "idle";
        const log = `${element.getAttribute("data-scroll-activity-log")} ${state}`;
        element.setAttribute("data-scroll-activity-log", log.trim());
        if (state === "idle" && log.includes("active")) observer.disconnect();
      });
      observer.observe(element, {
        attributeFilter: ["data-app-scrolling"],
        attributes: true,
      });
      element.scrollTop += 100;
    });
    await expect(scroller).toHaveAttribute(
      "data-scroll-activity-log",
      "active idle",
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

    const geometry = await page
      .locator(`${scrollport} [data-index="1"]`)
      .evaluate((row) => ({
        cardHeight:
          row
            .querySelector<HTMLElement>('[data-file-card="true"]')
            ?.getBoundingClientRect().height ?? 0,
        rowHeight: row.getBoundingClientRect().height,
      }));
    expect(geometry.cardHeight).toBeCloseTo(92, 0);
    expect(geometry.rowHeight).toBeCloseTo(104, 0);
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
