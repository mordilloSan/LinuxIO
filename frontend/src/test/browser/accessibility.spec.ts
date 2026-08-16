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

  test("uses the dock label instead of Chromium's descendant outline for keyboard focus", async ({
    page,
  }) => {
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");

    const dashboard = page.getByTestId("dock-dashboard");
    const label = dashboard.getByText("Dashboard");
    await expect(dashboard).toBeFocused();
    await expect(dashboard).not.toHaveAttribute("data-pointer-focus");
    await expect(dashboard).toHaveCSS("outline-style", "none");
    await expect(label).toHaveCSS("opacity", "1");
  });

  test("preserves pointer focus across window reactivation", async ({
    page,
  }) => {
    const dashboard = page.getByTestId("dock-dashboard");
    const label = dashboard.getByText("Dashboard");

    await dashboard.click();
    await expect(dashboard).toHaveAttribute("data-pointer-focus", "");
    await expect(label).toHaveCSS("opacity", "1");
    await page.keyboard.press("Alt");

    // A headed Chromium window emits window blur, then focusout while
    // document.hasFocus() is false. Headless pages do not have a desktop window
    // manager, so reproduce those measured event semantics directly.
    await dashboard.evaluate((element) => {
      window.dispatchEvent(new Event("blur"));
      const realHasFocus = document.hasFocus;
      Object.defineProperty(document, "hasFocus", {
        configurable: true,
        value: () => false,
      });
      element.dispatchEvent(new FocusEvent("focusout", { bubbles: true }));
      Object.defineProperty(document, "hasFocus", {
        configurable: true,
        value: realHasFocus,
      });
      window.dispatchEvent(new Event("focus"));
      element.dispatchEvent(new FocusEvent("focusin", { bubbles: true }));
    });
    await page.keyboard.press("Shift");

    await expect(dashboard).toBeFocused();
    await expect(dashboard).toHaveAttribute("data-pointer-focus", "");
    await expect(page.locator("html")).not.toHaveAttribute(
      "data-pointer-active",
    );
    await expect
      .poll(() =>
        dashboard.evaluate((element) => element.matches(":focus-visible")),
      )
      .toBe(true);
    await expect(dashboard).toHaveCSS("outline-style", "none");
    await expect(label).toHaveCSS("opacity", "0");
  });
});
