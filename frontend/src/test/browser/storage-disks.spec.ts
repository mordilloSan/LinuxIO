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
  const sda = page.getByRole("button", { name: "Drive details sda" }).first();
  await expect(sda).toContainText("Read 13 MB/s · Write 82 kB/s");
  // A partition nothing uses is a line on its drive's card.
  await expect(sda).toContainText("sda2 · No filesystem · 16 GB · not mounted");
  // The two EFI copies form one boot node; only the mounted copy streams.
  await expect(page.getByText("EFI boot")).toBeVisible();
  await expect(page.getByText("2 boot entries")).toBeVisible();
  await expect(page.getByText("2 copies · nvme0n1p1 mounted")).toBeVisible();
  const edges = page.locator(".storage-pools__edges .app-topology-edge");
  expect(await edges.count()).toBeGreaterThan(5);
  // sda reads at 12.5 MB/s into /srv/media: a medium stream; the network
  // share has no rate and idles; the spare EFI copy's line does not stream.
  await expect(
    edges.filter({ has: page.locator('[data-direction="reverse"]') }).first(),
  ).toHaveAttribute("data-tier", /light|medium/);
  expect(
    await edges.and(page.locator('[data-tier="medium"]')).count(),
  ).toBeGreaterThan(0);
  const idle = page
    .locator(
      '.storage-pools__edges .app-topology-edge[data-tier="idle"] .app-topology-edge__flow',
    )
    .first();
  const medium = page
    .locator(
      '.storage-pools__edges .app-topology-edge[data-tier="medium"] .app-topology-edge__flow',
    )
    .first();
  await expect(medium).toHaveCSS("animation-name", "app-topology-flow");
  await expect(idle).toHaveCSS("animation-name", "app-topology-flow");
  expect(
    await page
      .locator(
        ".storage-pools__edges .app-topology-edge:not([data-tier]) .app-topology-edge__flow",
      )
      .count(),
  ).toBe(0);
  await page.screenshot({
    path: testInfo.outputPath("storage-disks-activity.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "Pause animation" }).click();
  await expect(medium).toHaveCSS("animation-play-state", "paused");
  await expect(idle).toHaveCSS("animation-play-state", "paused");
  await page.getByRole("button", { name: "Resume animation" }).click();
  await expect(medium).toHaveCSS("animation-play-state", "running");
  await page.emulateMedia({ reducedMotion: "reduce" });
  await expect(medium).toHaveCSS("animation-name", "none");
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
