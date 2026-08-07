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
});
