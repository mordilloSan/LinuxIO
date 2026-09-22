import { expect, test } from "@playwright/test";

for (const scheme of ["dark", "light"] as const) {
  for (const width of [320, 1280]) {
    test(`shared dialog style in ${scheme} at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      await page.emulateMedia({ reducedMotion: "reduce" });
      await page.goto(`/styling/${scheme}/dialogs`);
      await page
        .getByRole("button", { name: "Open form", exact: true })
        .click();
      const dialog = page.getByRole("dialog", { name: "Configure interface" });
      await expect(dialog).toBeVisible();
      await page.evaluate(() => document.fonts.ready);
      for (const label of [
        "Interface name",
        "NIC",
        "DNS (optional)",
        "Port",
        "Protocol",
        "Owner",
      ]) {
        await expect(dialog.getByLabel(label, { exact: true })).toHaveCSS(
          "font-size",
          "14px",
        );
      }
      await expect(
        dialog.locator('.app-text-field__label[data-shrink="false"]'),
      ).toHaveCSS("font-size", "14px");
      await expect(dialog.locator(".app-dialog-title")).toHaveCSS(
        "font-size",
        "16px",
      );
      const geometry = await dialog.evaluate((element) => ({
        width: element.clientWidth,
        scrollWidth: element.scrollWidth,
        right: element.getBoundingClientRect().right,
      }));
      expect(geometry.scrollWidth).toBeLessThanOrEqual(geometry.width);
      expect(geometry.right).toBeLessThanOrEqual(width);
      await expect(
        dialog.getByRole("button", { name: "Save changes" }),
      ).toBeInViewport();
      await expect(dialog).toHaveScreenshot(`dialog-${scheme}-${width}.png`, {
        animations: "disabled",
      });

      // Long field labels must ellipsize within the control after scaling.
      const longLabel = dialog.locator(".app-text-field__label").first();
      await longLabel.evaluate((element) => {
        element.textContent =
          "Interface name with a long description that must stay inside the field";
      });
      expect(
        await longLabel.evaluate(
          (element) =>
            element.getBoundingClientRect().right <=
            element.parentElement!.getBoundingClientRect().right,
        ),
      ).toBe(true);

      await dialog.getByLabel("NIC", { exact: true }).click();
      const option = page.getByRole("option", {
        name: "eth1 (192.168.1.10/24)",
      });
      await expect(option).toHaveCSS("font-size", "14px");
      await option.click();
      await expect(dialog.getByLabel("NIC", { exact: true })).toHaveText(
        "eth1 (192.168.1.10/24)",
      );
      await dialog.getByLabel("Owner", { exact: true }).fill("bob");
      const ownerOption = page.getByRole("option", {
        name: "bob",
        exact: true,
      });
      await expect(ownerOption).toHaveCSS("font-size", "14px");
      await ownerOption.click();
      await dialog.getByRole("button", { name: "Cancel" }).click();
      await expect(dialog).toBeHidden();
      await expect(
        page.getByRole("button", { name: "Open form", exact: true }),
      ).toBeFocused();
      await expect(page.getByLabel("Outside dialog")).toHaveCSS(
        "font-size",
        "16px",
      );
    });
  }
}

test("fullscreen inherits the same fields and named title", async ({
  page,
}) => {
  await page.goto("/styling/dark/dialogs");
  await page
    .getByRole("button", { name: "Open fullscreen", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "Fullscreen settings" });
  for (const label of ["Interface name", "NIC", "Port", "Protocol", "Owner"]) {
    await expect(dialog.getByLabel(label, { exact: true })).toHaveCSS(
      "font-size",
      "14px",
    );
  }
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(
    page.getByRole("button", { name: "Open fullscreen", exact: true }),
  ).toBeFocused();
});

test("confirmation actions remain reachable at 320px", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 });
  await page.goto("/styling/dark/dialogs");
  await page
    .getByRole("button", { name: "Open confirmation", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "Unsaved Changes" });
  for (const name of ["Keep Editing", "Discard and Exit", "Save and Exit"]) {
    const action = dialog.getByRole("button", { name, exact: true });
    await expect(action).toBeInViewport();
    const rect = await action.boundingBox();
    expect(rect!.x).toBeGreaterThanOrEqual(0);
    expect(rect!.x + rect!.width).toBeLessThanOrEqual(320);
  }
  await expect(
    dialog.getByRole("button", { name: "Save and Exit" }),
  ).toHaveClass(/app-btn--contained/);
  await dialog.getByRole("button", { name: "Keep Editing" }).click();
  await expect(dialog).toBeHidden();
});

test("long forms scroll without losing their actions", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 400 });
  await page.goto("/styling/dark/dialogs");
  await page.getByRole("button", { name: "Open form", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Configure interface" });
  const content = dialog.locator(".app-dialog-content");
  expect(
    await content.evaluate(
      (element) => element.scrollHeight > element.clientHeight,
    ),
  ).toBe(true);
  await expect(
    dialog.getByRole("button", { name: "Save changes" }),
  ).toBeInViewport();
  await dialog.getByLabel("Owner", { exact: true }).scrollIntoViewIfNeeded();
  await expect(dialog.getByLabel("Owner", { exact: true })).toBeInViewport();
  await expect(dialog.getByRole("button", { name: "Cancel" })).toBeInViewport();
});

test("pending confirmation keeps its button label on mobile", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 400 });
  await page.goto("/styling/dark/dialogs");
  await page
    .getByRole("button", { name: "Open pending confirmation", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "Delete items" });
  const action = dialog.getByRole("button", { name: "Deleting…" });
  await expect(action).toBeDisabled();
  await expect(action.locator(".app-btn__label")).toBeVisible();
  await expect(action).toBeInViewport();
  await expect(dialog).toHaveAttribute("aria-busy", "true");
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
});
