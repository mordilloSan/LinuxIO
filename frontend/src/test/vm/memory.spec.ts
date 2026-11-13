import { expect, type Locator, type Page, test } from "@playwright/test";

import { signIn, signOutIfSignedIn, vmSsh } from "./vm";

// The monitoring agent splits Docker and VM memory out of "used" by reading
// cgroups. The VM has Docker but no libvirt, so a transient unit in
// machine.slice, the slice libvirt puts each guest in, stands in for a guest.
const CONTAINER = "e2e-memory";
const GUEST = "e2e-guest";
const MiB = 1024 * 1024;
const UNITS: Record<string, number> = {
  Bytes: 1,
  KB: 1024,
  MB: MiB,
  GB: 1024 * MiB,
};

const cleanUp = () =>
  vmSsh(
    `sudo docker rm -f ${CONTAINER} >/dev/null 2>&1; sudo systemctl stop ${GUEST} >/dev/null 2>&1; true`,
  );

test.beforeAll(async () => {
  await cleanUp();
  // The hardware page, which holds the memory history, requires lm-sensors.
  await vmSsh(
    "command -v sensors >/dev/null || sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq lm-sensors >/dev/null",
  );
  // tmpfs pages are shared memory the container cannot drop without swap.
  await vmSsh(
    `sudo docker run -d --name ${CONTAINER} --tmpfs /hold:size=300m busybox:1.37 ` +
      `sh -c 'dd if=/dev/zero of=/hold/fill bs=1M count=200 2>/dev/null; exec sleep 3600'`,
  );
  await vmSsh(
    `sudo systemd-run --quiet --slice=machine.slice --unit=${GUEST} ` +
      `python3 -c "import time; held = bytes(range(256)) * (300 << 12); time.sleep(3600)"`,
  );
});

test.afterAll(cleanUp);

test.afterEach(async ({ page }) => signOutIfSignedIn(page));

/** The bytes a "Memory Usage" row shows, parsed from formatFileSize text. */
async function rowBytes(card: Locator, label: string): Promise<number> {
  const text = await card
    .locator(".info-row")
    .filter({ hasText: new RegExp(`^${label}`) })
    .innerText();
  const match = text.match(/([\d.]+)\s*(Bytes|KB|MB|GB)/);
  return match ? Number(match[1]) * UNITS[match[2]] : 0;
}

const memoryCard = (page: Page) =>
  page
    .getByText("Memory Usage", { exact: true })
    .locator("xpath=ancestor::*[.//*[contains(@class,'info-row')]][1]");

test("splits Docker and VM memory out of used memory", async ({ page }) => {
  test.setTimeout(240_000);
  await signIn(page);

  const card = memoryCard(page);
  await expect
    .poll(() => rowBytes(card, "VMs"), { timeout: 30_000 })
    .toBeGreaterThan(280 * MiB);
  await expect
    .poll(() => rowBytes(card, "Docker"), { timeout: 30_000 })
    .toBeGreaterThan(190 * MiB);
  await card.screenshot({ path: test.info().outputPath("memory-card.png") });

  // The history band needs a collector tick (one minute) after the holders
  // started; the one-hour range refetches every 15 seconds.
  await page.goto("/hardware");
  const chart = page.getByRole("img", {
    name: "System, Docker, VMs history chart",
  });
  await expect(chart).toBeVisible({ timeout: 150_000 });

  const box = await chart.boundingBox();
  expect(box).not.toBeNull();
  await chart.hover({ position: { x: box!.width - 8, y: box!.height / 2 } });
  // Hover is synchronized, so every history card shows a tooltip.
  const tooltip = chart.locator(".chart-tooltip-box");
  await expect(tooltip).toContainText("System");
  await expect(tooltip).toContainText("Docker");
  await expect(tooltip).toContainText("VMs");
  // Hovering scrolled the chart into view, so measure it again.
  const shown = (await chart.boundingBox())!;
  await page.screenshot({
    path: test.info().outputPath("memory-history.png"),
    clip: {
      x: Math.max(shown.x - 40, 0),
      y: Math.max(shown.y - 90, 0),
      width: shown.width + 80,
      height: shown.height + 130,
    },
  });
});
