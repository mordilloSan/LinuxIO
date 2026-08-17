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

  // Escape put the user on the keyboard, so the restored focus wears the
  // designed icon-button ring — an intentional indicator, not an artifact.
  await expect(actions).toBeFocused();
  await expect(actions).toHaveCSS("outline-style", "solid");
  await expect(actions).toHaveCSS("outline-width", "2px");
});
