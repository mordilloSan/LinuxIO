import { expect, test } from "@playwright/test";

for (const theme of ["dark", "light"] as const) {
  test(`storage topology traces devices and application paths in ${theme}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 1100 });
    await page.goto(
      theme === "dark"
        ? "/storage/topology"
        : "/styling/light/storage-topology",
    );
    await expect(
      page.getByText("Live activity", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", {
        name: "Inspect mount point /srv/appdata",
        exact: true,
      })
      .click();
    const inspector = page.getByRole("complementary", {
      name: "Storage details",
    });
    await expect(
      inspector.getByRole("heading", { name: "/srv/appdata", exact: true }),
    ).toBeVisible();
    await expect(
      inspector.getByText("/dev/nvme0n1p2", { exact: true }),
    ).toBeVisible();
    await expect(
      inspector.getByRole("link", { name: "Open LVM" }),
    ).toHaveAttribute("href", "/storage/lvm");
    expect(new URL(page.url()).searchParams.get("node")).toBe(
      "mount:/srv/appdata",
    );
    await expect(
      page.locator('.storage-topology__edge[data-highlighted="true"]'),
    ).toHaveCount(5);
    await page.screenshot({
      path: testInfo.outputPath(`storage-topology-${theme}.png`),
      fullPage: true,
      animations: "disabled",
    });
    await inspector
      .getByRole("button", { name: "jellyfin", exact: true })
      .click();
    await expect(
      inspector.getByRole("button", { name: "/srv/media", exact: true }),
    ).toBeVisible();
    await expect(inspector.getByText("→ /media · Read only")).toBeVisible();
    await page.goBack();
    await expect(
      inspector.getByRole("heading", { name: "/srv/appdata", exact: true }),
    ).toBeVisible();
    await page.reload();
    await expect(
      inspector.getByRole("heading", { name: "/srv/appdata", exact: true }),
    ).toBeVisible();
  });
}

test("storage topology supports keyboard, pausing, and reduced motion", async ({
  page,
}) => {
  await page.goto("/storage/topology");
  const pause = page.getByRole("button", { name: "Pause animation" });
  await expect(pause).toBeVisible();
  await page.keyboard.press("Tab");
  await expect(pause).toBeFocused();
  await page.keyboard.press("Enter");
  await page.keyboard.press("Tab");
  const drive = page.getByRole("button", { name: "Inspect device nvme0n1" });
  await expect(drive).toBeFocused();
  expect(
    await drive.evaluate((element) => getComputedStyle(element).outlineStyle),
  ).toBe("solid");
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("heading", { name: "nvme0n1", exact: true }),
  ).toBeFocused();
  const rate = page
    .locator('.storage-topology__rate[data-active="true"]')
    .first();
  expect(
    await rate.evaluate(
      (element) => getComputedStyle(element, "::after").animationName,
    ),
  ).toBe("none");
  await page.getByRole("button", { name: "Resume animation" }).click();
  expect(
    await rate.evaluate(
      (element) => getComputedStyle(element, "::after").animationName,
    ),
  ).toBe("storage-topology-activity");
  await page.emulateMedia({ reducedMotion: "reduce" });
  expect(
    await rate.evaluate(
      (element) => getComputedStyle(element, "::after").animationName,
    ),
  ).toBe("none");
  await expect(rate).toContainText(/Read.*s/);
});

test("storage topology reflows at 320px and a 200% desktop zoom equivalent", async ({
  page,
}, testInfo) => {
  await page.goto("/storage/topology");
  for (const width of [640, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await page
      .getByRole("button", {
        name: "Inspect mount point /srv/media",
        exact: true,
      })
      .click();
    const inspector = page.getByRole("complementary", {
      name: "Storage details",
    });
    await expect(
      inspector.getByRole("heading", { name: "/srv/media", exact: true }),
    ).toBeInViewport();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    await inspector
      .getByRole("button", { name: "jellyfin", exact: true })
      .click();
    await expect(inspector.getByText("→ /media · Read only")).toBeVisible();
  }
  await page.screenshot({
    path: testInfo.outputPath("storage-topology-mobile.png"),
    fullPage: true,
    animations: "disabled",
  });
});
