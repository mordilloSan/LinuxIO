import { expect, test } from "@playwright/test";

/**
 * The routed tab strip is `position: sticky` against MainLayout's scrollport.
 * Two things about that only exist under a real layout engine:
 *
 *  - a sticky box cannot leave its containing block, so the strip stays put
 *    only for as long as `.tab-container` is on screen. Sized to one scrollport
 *    it ran out, and a page taller than that dragged the strip off the top;
 *  - the strip discards the page's top inset and sits flush under the header
 *    as a fixed-height bar; the gap from the header to the pills is the bar's
 *    own centering and must not drift over the first scrolled pixels the way
 *    the old carried-inset model could.
 *
 * Both are geometry, so both are measured here rather than in jsdom.
 */

/** Distance from the header's bottom edge to the top of the tab pills. */
async function gapUnderHeader(page: import("@playwright/test").Page) {
  return page.evaluate(() => {
    const header = document.querySelector('[data-testid="fixture-header"]')!;
    const pills = document.querySelector('[role="tablist"]')!;
    return Math.round(
      pills.getBoundingClientRect().top - header.getBoundingClientRect().bottom,
    );
  });
}

async function scrollTo(page: import("@playwright/test").Page, top: number) {
  await page.evaluate((target) => {
    const scrollport = document.querySelector<HTMLElement>(
      '[data-testid="fixture-scrollport"]',
    )!;
    scrollport.scrollTop = target;
  }, top);
  // Sticky offsets settle on the frame after the scroll.
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );
}

function maxScroll(page: import("@playwright/test").Page) {
  return page.evaluate(() => {
    const scrollport = document.querySelector<HTMLElement>(
      '[data-testid="fixture-scrollport"]',
    )!;
    return scrollport.scrollHeight - scrollport.clientHeight;
  });
}

test.describe("routed tab strip", () => {
  test("stays put over a page taller than the scrollport", async ({ page }) => {
    await page.goto("/scrolling-tabs/grow");
    await expect(page.getByRole("tab", { name: "Groups" })).toBeVisible();

    const limit = await maxScroll(page);
    expect(limit).toBeGreaterThan(0);

    const resting = await gapUnderHeader(page);
    expect(resting).toBeGreaterThan(0);

    for (const top of [5, 20, 60, Math.round(limit / 2), limit]) {
      await scrollTo(page, top);
      // The strip neither drifts toward the header nor scrolls off it.
      expect
        .soft(await gapUnderHeader(page), `gap at scrollTop ${top}`)
        .toBe(resting);
      await expect
        .soft(page.getByRole("tab", { name: "Groups" }))
        .toBeInViewport();
    }
  });

  test("keeps a virtualized panel filling the scrollport", async ({ page }) => {
    await page.goto("/scrolling-tabs/fill");
    await expect(page.getByRole("tab", { name: "Groups" })).toBeVisible();

    // The panel owns its overflow, so the page itself must not scroll — the
    // tab strip and the header stay exactly where they are.
    expect(await maxScroll(page)).toBe(0);
    await expect(page.getByRole("grid")).toBeVisible();

    const grid = await page.evaluate(() => {
      const element = document.querySelector<HTMLElement>('[role="grid"]')!;
      return {
        height: Math.round(element.getBoundingClientRect().height),
        scrollable: element.scrollHeight - element.clientHeight,
      };
    });
    expect(grid.height).toBeGreaterThan(100);
    expect(grid.scrollable).toBeGreaterThan(0);
  });

  test("keeps a filling card grid scrolling inside the panel", async ({
    page,
  }) => {
    await page.goto("/scrolling-tabs/cards");
    await expect(page.getByRole("tab", { name: "Groups" })).toBeVisible();
    await expect(page.getByText("item-0")).toBeVisible();

    // Same contract as the virtualized grid: cards move, page does not.
    expect(await maxScroll(page)).toBe(0);

    const grid = await page.evaluate(() => {
      const cards = document.querySelector<HTMLElement>(".app-grid")!;
      const scroller = cards.parentElement!;
      const style = getComputedStyle(scroller);
      return {
        overflow: style.overflowY,
        scrollable: scroller.scrollHeight - scroller.clientHeight,
        // The reserved lift headroom and shadow gutter are handed straight
        // back, so the cards sit exactly where an unfilled grid puts them.
        insetTop:
          Math.round(cards.getBoundingClientRect().top) -
          Math.round(scroller.getBoundingClientRect().top),
        insetLeft:
          Math.round(cards.getBoundingClientRect().left) -
          Math.round(scroller.getBoundingClientRect().left),
        marginTop: style.marginTop,
        marginLeft: style.marginLeft,
        paddingTop: style.paddingTop,
        paddingLeft: style.paddingLeft,
      };
    });

    expect(grid.overflow).toBe("auto");
    expect(grid.scrollable).toBeGreaterThan(0);
    // Reserved inside, given back outside: the pair must cancel exactly.
    expect(grid.paddingTop).toBe(`${grid.insetTop}px`);
    expect(grid.marginTop).toBe(`-${grid.insetTop}px`);
    expect(grid.paddingLeft).toBe(`${grid.insetLeft}px`);
    expect(grid.marginLeft).toBe(`-${grid.insetLeft}px`);
  });
});
