import { expect, test } from "@playwright/test";

test("storage disks clear the drive, volume and RAID selection with Escape", async ({
  page,
}) => {
  await page.goto("/storage");
  // nvme0n1 holds /boot/efi and, through LVM, / and /srv/appdata.
  await page
    .getByRole("button", { name: "Drive details nvme0n1" })
    .first()
    .click();
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
    page.getByRole("button", { name: "Drive details nvme0n1" }).first(),
  ).toBeVisible();

  await page.goto("/storage?raid=md127");
  await expect(
    page.getByRole("button", { name: "Drive details nvme0n1" }).first(),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page).toHaveURL(/\/storage$/);
});
