import { expect, test } from "@playwright/test";

const controls = [
  "Activate button",
  "Activate icon button",
  "Activate chip",
] as const;

test.describe("accessibility fixture controls", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/accessibility");
    await expect(
      page.getByRole("heading", { name: "Accessibility fixture" }),
    ).toBeVisible();
  });

  test("reaches every control with Tab and shows a visible focus outline", async ({
    page,
  }) => {
    for (const name of controls) {
      await page.keyboard.press("Tab");
      const active = page.getByRole("button", { name });
      await expect(active).toBeFocused();
      await expect(active).toHaveCSS("outline-style", "solid");
      await expect(active).toHaveCSS("outline-width", "2px");
    }
  });

  test("activates each control with Enter and Space", async ({ page }) => {
    for (const name of controls) {
      await page.keyboard.press("Tab");
      await expect(page.getByRole("button", { name })).toBeFocused();
      await page.keyboard.press("Enter");
      await page.keyboard.press("Space");
    }

    await expect(page.getByTestId("activation-counts")).toHaveText(
      "Button: 2; Icon: 2; Chip: 2",
    );
  });

  test("does not scroll when Space activates the chip", async ({ page }) => {
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");
    await expect(
      page.getByRole("button", { name: "Activate chip" }),
    ).toBeFocused();

    await page.evaluate(() => window.scrollTo(0, 500));
    const scrollBefore = await page.evaluate(() => window.scrollY);
    await page.keyboard.press("Space");
    await expect
      .poll(() => page.evaluate(() => window.scrollY))
      .toBe(scrollBefore);
  });

  test("uses a designed tile ring instead of Chromium's descendant outline for keyboard focus", async ({
    page,
  }) => {
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");

    const dashboard = page.getByTestId("dock-dashboard");
    const tile = dashboard.locator(".app-dock__tile");
    const label = dashboard.getByText("Dashboard");
    await expect(dashboard).toBeFocused();
    // Chromium's default anchor outline would trace the transformed tile and
    // the absolutely positioned label as one stepped contour; the deliberate
    // ring lives on the tile alone, with the label as its caption.
    await expect(dashboard).toHaveCSS("outline-style", "none");
    await expect(tile).toHaveCSS("outline-style", "solid");
    // Authored 3.2px; Chromium snaps the used width to whole device pixels.
    await expect(tile).toHaveCSS("outline-width", /^3(\.2)?px$/);
    await expect(label).toHaveCSS("opacity", "1");
  });

  test("requires live pointer movement before hover may show a dock label", async ({
    page,
  }) => {
    const dock = page.getByRole("navigation", { name: "Dock fixture" });
    const dashboard = page.getByTestId("dock-dashboard");
    const label = dashboard.getByText("Dashboard");

    await dashboard.hover();
    await expect(dock).toHaveAttribute("data-dock-pointer", "");
    await expect(label).toHaveCSS("opacity", "1");

    // Losing the window clears the gate. Chromium revives :hover when the
    // window returns without dispatching a pointermove, so the label must
    // stay hidden until the pointer actually moves again.
    await page.evaluate(() => window.dispatchEvent(new Event("blur")));
    await expect(dock).not.toHaveAttribute("data-dock-pointer");
    await expect(label).toHaveCSS("opacity", "0");

    const box = await dashboard.boundingBox();
    if (!box) throw new Error("dock link has no layout box");
    await page.mouse.move(box.x + box.width / 2 + 2, box.y + box.height / 2);
    await expect(dock).toHaveAttribute("data-dock-pointer", "");
    await expect(label).toHaveCSS("opacity", "1");
  });

  test("summons tooltips for keyboard focus on non-text controls only", async ({
    page,
  }) => {
    for (let i = 0; i < 5; i += 1) await page.keyboard.press("Tab");
    await expect(
      page.getByRole("button", { name: "Tooltip button" }),
    ).toBeFocused();
    await expect(page.getByRole("tooltip")).toHaveText("Collapse row");

    // Text entry is excluded: it matches :focus-visible on any focus by
    // design, and a bubble parked over the caret helps nobody.
    await page.keyboard.press("Tab");
    await expect(
      page.getByRole("textbox", { name: "Tooltip query" }),
    ).toBeFocused();
    await page.waitForTimeout(250);
    await expect(page.getByRole("tooltip")).toHaveCount(0);
  });

  test("does not park a tooltip on pointer-taken or pointer-restored focus", async ({
    page,
  }) => {
    const trigger = page.getByRole("button", { name: "Tooltip button" });

    // A click hovers first, so the bubble appears — but leaving must clear it
    // even though the trigger keeps focus.
    await trigger.click();
    await page.mouse.move(0, 0);
    await expect(page.getByRole("tooltip")).toHaveCount(0);
    await expect(trigger).toBeFocused();

    // A later keystroke may make the trigger match :focus-visible, but no
    // focus event fires, so nothing summons the bubble.
    await page.keyboard.press("Shift");
    await page.waitForTimeout(250);
    await expect(page.getByRole("tooltip")).toHaveCount(0);

    // Programmatic restoration after pointer interaction — a closing dialog.
    // Script focus inherits the non-matching :focus-visible state, so no
    // bubble appears with the pointer somewhere else entirely.
    await page.getByRole("button", { name: "Activate button" }).click();
    await page.mouse.move(0, 0);
    await trigger.evaluate((element) => (element as HTMLElement).focus());
    await expect(trigger).toBeFocused();
    await page.waitForTimeout(250);
    await expect(page.getByRole("tooltip")).toHaveCount(0);
  });

  test("summons the tooltip for focus restored after keyboard interaction", async ({
    page,
  }) => {
    // Tab somewhere (keyboard modality), then restore focus to the trigger the
    // way a closing dialog would. The user is on the keyboard — a blur is
    // coming — so the bubble may appear; this is accepted behavior, not a bug.
    await page.keyboard.press("Tab");
    const trigger = page.getByRole("button", { name: "Tooltip button" });
    await trigger.evaluate((element) => (element as HTMLElement).focus());
    await expect(page.getByRole("tooltip")).toHaveText("Collapse row");
  });
});
