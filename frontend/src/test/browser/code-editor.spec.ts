import { expect, test } from "@playwright/test";

test("loads, edits, saves, and releases Tab focus in CodeMirror", async ({
  page,
}) => {
  await page.goto("/editor");
  await expect(
    page.getByRole("heading", { name: "Code editor fixture" }),
  ).toBeVisible();

  const editor = page.getByRole("textbox", { name: "Code editor" });
  await expect(editor).toBeVisible();
  await expect(page.locator(".tok-propertyName")).toHaveText('"enabled"');

  await editor.click();
  await page.keyboard.press("Control+End");
  await page.keyboard.insertText('\n"port": 8080');
  await page.keyboard.press("Control+s");
  await expect(page.getByTestId("saved-content")).toContainText('"port": 8080');

  await editor.focus();
  await page.keyboard.press("Tab");
  await expect(
    page.getByRole("button", { name: "After editor" }),
  ).toBeFocused();
});
