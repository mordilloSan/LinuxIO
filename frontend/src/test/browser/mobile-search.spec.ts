import { expect, test } from "@playwright/test";

test("restores mobile search focus without inventing a pointer focus ring", async ({
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

  await expect(actions).toBeFocused();
  await expect(actions).toHaveAttribute("data-pointer-focus", "");
  await expect(actions).toHaveCSS("outline-style", "none");

  // Keyboard-opened search keeps the normal focus ring when focus returns.
  await page.keyboard.press("Enter");
  const searchAction = page.getByRole("button", { name: "Search" });
  await searchAction.focus();
  await page.keyboard.press("Enter");
  await expect(search).toBeFocused();
  await page.keyboard.press("Escape");

  await expect(actions).toBeFocused();
  await expect(actions).not.toHaveAttribute("data-pointer-focus");
  await expect(actions).toHaveCSS("outline-style", "solid");
  await expect(actions).toHaveCSS("outline-width", "2px");
});
