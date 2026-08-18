import { expect, test } from "@playwright/test";

test("restores focus to the Actions trigger when mobile search closes", async ({
  page,
}) => {
  await page.setViewportSize({ width: 380, height: 665 });
  await page.goto("/accounts");
  await expect(
    page.getByRole("heading", { name: "Users route content" }),
  ).toBeVisible();

  const actions = page.getByRole("button", { name: "Actions" });
  await actions.click();
  await page.getByRole("button", { name: "Search" }).click();

  const search = page.getByRole("textbox", { name: "Search users" });
  await expect(search).toBeFocused();
  await search.pressSequentially("apparmor");
  await page.keyboard.press("Escape");
  await expect(search).toHaveValue("");
  await expect(search).toBeFocused();
  await page.keyboard.press("Escape");

  // Focus returns to its stable owner, but Escape does not opt into navigation
  // presentation. Only a prior Tab press may paint the trigger ring.
  await expect(actions).toBeFocused();
  await expect(actions).toHaveCSS("outline-style", "none");
});
