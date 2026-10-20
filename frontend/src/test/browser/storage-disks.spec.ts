import { expect, test } from "@playwright/test";

test("storage disks clear the drive and volume selection with Escape", async ({
  page,
}) => {
  await page.goto("/storage");
  await page.getByRole("button", { name: "Drive details nvme0n1" }).click();
  await expect(page).toHaveURL(/\?drive=nvme0n1$/);
  const card = page.locator("button[aria-expanded]", { hasText: "nvme0n1" });
  await expect(card).toHaveAttribute("aria-expanded", "true");
  await page.getByRole("tab").nth(1).click();
  await page.keyboard.press("Escape");
  await expect(page).toHaveURL(/\/storage$/);

  await page.getByRole("button", { name: "Volume /srv/media" }).click();
  await expect(page).toHaveURL(/\?fs=/);
  await page.keyboard.press("Escape");
  await expect(page).toHaveURL(/\/storage$/);
  await expect(
    page.getByRole("button", { name: "Drive details nvme0n1" }),
  ).toBeVisible();
});
