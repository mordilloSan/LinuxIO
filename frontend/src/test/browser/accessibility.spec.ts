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

  test("does not restore a stale dock hover after window reactivation", async ({
    context,
    page,
  }) => {
    const settings = page.getByTestId("dock-settings");
    const label = settings.getByText("Settings");

    await settings.click();
    await expect(settings).toHaveAttribute("data-pointer-focus", "");
    await expect(label).toHaveCSS("opacity", "1");

    // Chromium restores :hover when the page comes back, but no pointermove is
    // dispatched. That used to reveal this label while the motion-driven dock
    // itself remained at rest — the exact disconnected box seen in the app.
    const otherPage = await context.newPage();
    await otherPage.setContent("<title>Other task</title>");
    // Headless pages do not share a desktop window manager, so bringing the
    // second page forward does not produce the OS window's blur event.
    await page.evaluate(() => window.dispatchEvent(new Event("blur")));
    await page.bringToFront();

    await expect(settings).toHaveAttribute("data-pointer-focus", "");
    await expect(page.locator("html")).not.toHaveAttribute(
      "data-pointer-active",
    );
    await expect(label).toHaveCSS("opacity", "0");

    const box = await settings.boundingBox();
    expect(box).not.toBeNull();
    await page.mouse.move(box!.x + 1, box!.y + 1);
    await expect(label).toHaveCSS("opacity", "1");
    await otherPage.close();
  });
});
