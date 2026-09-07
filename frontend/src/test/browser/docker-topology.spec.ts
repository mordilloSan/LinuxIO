import { expect, test } from "@playwright/test";

import { topologyNextcloudId } from "../dockerTopologyFixture";

for (const scheme of ["dark", "light"] as const) {
  test(`topology supports inspection and stack navigation in ${scheme}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(
      scheme === "light" ? "/styling/light/topology" : "/docker/topology",
    );
    await expect(
      page.getByText("Live activity", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Inspect container nextcloud" })
      .click();
    expect(new URL(page.url()).searchParams.get("container")).toBe(
      topologyNextcloudId,
    );
    const details = page.getByRole("region", { name: "Topology details" });
    await expect(details.getByText("172.20.0.2/16")).toBeVisible();
    await expect(details.getByText("1.2%", { exact: true })).toBeVisible();
    await expect(details.getByText("128 MB", { exact: true })).toBeVisible();
    await expect(details.locator(".docker-topology__rate--rx")).toContainText(
      "18 kB/s",
    );
    await expect(details.locator(".docker-topology__rate--tx")).toContainText(
      "80 kB/s",
    );
    await expect(page.locator('path[data-highlighted="true"]')).toHaveCount(2);
    await page.getByRole("button", { name: "Collapse stack cloud" }).click();
    await expect(
      page.getByRole("button", { name: "Inspect container nextcloud" }),
    ).toHaveCount(0);
    await expect(details.getByText("nextcloud:31")).toBeVisible();
    await page.getByRole("button", { name: "Expand stack cloud" }).click();
    await details.getByRole("button", { name: "cloud_internal" }).click();
    await expect(page).toHaveURL(/network=cloud-id/);
    await expect(page.locator('path[data-highlighted="true"]')).toHaveCount(3);
    await page.goBack();
    await expect(
      page.getByRole("button", { name: "Inspect container nextcloud" }),
    ).toHaveAttribute("aria-pressed", "true");
    await page.reload();
    await expect(details.getByText("nextcloud:31")).toBeVisible();
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({
      path: testInfo.outputPath(`topology-${scheme}.png`),
      fullPage: true,
      animations: "disabled",
    });
  });
}

test("topology has keyboard focus and can stop all activity animation", async ({
  page,
}) => {
  await page.goto("/docker/topology");
  const pause = page.getByRole("button", { name: "Pause animation" });
  await expect(pause).toBeVisible();
  await page.keyboard.press("Tab");
  await expect(pause).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("button", { name: "Resume animation" }),
  ).toHaveAttribute("aria-pressed", "true");
  await page.keyboard.press("Tab");
  const network = page.getByRole("button", { name: "Inspect network bridge" });
  await expect(network).toBeFocused();
  expect(
    await network.evaluate((element) => getComputedStyle(element).outlineStyle),
  ).toBe("solid");
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("heading", { name: "bridge", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Live activity", { exact: true })).toBeVisible();
  const rate = page
    .locator('.docker-topology__rate[data-active="true"]')
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
  ).toBe("docker-topology-activity");
  await page.emulateMedia({ reducedMotion: "reduce" });
  expect(
    await rate.evaluate(
      (element) => getComputedStyle(element, "::after").animationName,
    ),
  ).toBe("none");
  await expect(rate).toContainText(/s/);
});

test("topology reflows at 320px and at a 200% desktop zoom equivalent", async ({
  page,
}, testInfo) => {
  await page.goto(`/docker/topology?container=${topologyNextcloudId}`);
  for (const width of [640, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    const details = page.getByRole("region", { name: "Topology details" });
    await details.scrollIntoViewIfNeeded();
    await expect(details.getByText("172.21.0.2/16")).toBeVisible();
    await details.getByRole("button", { name: "cloud_internal" }).click();
    await expect(
      details.getByRole("heading", { name: "cloud_internal" }),
    ).toBeVisible();
    await expect(
      details.getByRole("heading", { name: "cloud_internal" }),
    ).toBeInViewport();
    await details
      .getByRole("button", { name: "nextcloud", exact: true })
      .first()
      .click();
    await expect(details.getByText("nextcloud:31")).toBeVisible();
  }
  await page.screenshot({
    path: testInfo.outputPath("topology-mobile.png"),
    fullPage: true,
    animations: "disabled",
  });
});
