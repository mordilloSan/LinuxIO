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

test("storage disks carry live reads and writes on their lines", async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.goto("/storage");
  await expect(
    page.getByRole("button", { name: "Drive details sda" }).first(),
  ).toContainText("Read 13 MB/s · Write 82 kB/s");
  await expect(
    page.locator(".storage-pools__card", { hasText: "sda2" }),
  ).toContainText("Not mounted · 16 GBNo filesystem");
  const grain = page
    .locator(
      '.app-topology-edge__flow[data-direction="forward"] .app-topology-edge__grain',
    )
    .first();
  const reverse = page
    .locator(
      '.app-topology-edge__flow[data-direction="reverse"] .app-topology-edge__grain',
    )
    .first();
  await expect(grain).toHaveCSS("animation-name", "app-topology-flow");
  await expect(reverse).toHaveCSS("animation-direction", "reverse");
  await page.screenshot({
    path: testInfo.outputPath("storage-disks-activity.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "Pause animation" }).click();
  await expect(grain).toHaveCSS("animation-play-state", "paused");
  await expect(reverse).toHaveCSS("animation-play-state", "paused");
  await page.getByRole("button", { name: "Resume animation" }).click();
  await expect(grain).toHaveCSS("animation-play-state", "running");
  await page.emulateMedia({ reducedMotion: "reduce" });
  await expect(grain).toHaveCSS("animation-name", "none");
});

test("storage disks list partitions and offer unmount from fstab", async ({
  page,
}) => {
  await page.goto("/storage?drive=sda");
  await page.getByRole("tab", { name: "Partitions" }).click();
  await expect(page.getByText("No partition table")).toBeVisible();
  await expect(page.getByText("sda2", { exact: true })).toBeVisible();

  await page.goto("/storage?fs=%2Fsrv%2Fmedia");
  await page.getByRole("button", { name: "Unmount", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Unmount filesystem" });
  await expect(
    dialog.getByLabel("Also remove from /etc/fstab"),
  ).not.toBeChecked();
});
