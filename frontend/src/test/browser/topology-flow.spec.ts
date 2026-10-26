import { expect, test } from "@playwright/test";

const views = [
  {
    name: "docker",
    path: "/docker/topology",
    edges: ".docker-topology__edges",
  },
  { name: "storage", path: "/storage", edges: ".storage-pools__edges" },
];

for (const view of views)
  test(`${view.name} streams move, pause in place, and respect reduced motion`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 1100 });
    await page.emulateMedia({ reducedMotion: "no-preference" });
    await page.goto(view.path);
    // A horizontal SVG path has zero bounding-box height even with a visible stroke.
    await expect(page.locator(view.edges)).toBeVisible();
    const flows = page.locator(`${view.edges} .app-topology-edge__flow`);
    const forward = flows
      .and(page.locator('[data-direction="forward"]'))
      .first();
    const reverse = flows
      .and(page.locator('[data-direction="reverse"]'))
      .first();
    // Every live edge streams, at a pace set by its tier.
    const tiers = await page
      .locator(`${view.edges} .app-topology-edge[data-tier]`)
      .evaluateAll((elements) =>
        elements.map((element) => element.getAttribute("data-tier")),
      );
    expect(tiers.length).toBeGreaterThan(1);
    // Where the fixture has both an idle and a measured edge, the measured
    // one moves faster.
    const measured = tiers.find((tier) => tier !== "idle");
    if (measured && tiers.includes("idle")) {
      const period = (tier: string) =>
        page
          .locator(
            `${view.edges} .app-topology-edge[data-tier="${tier}"] .app-topology-edge__flow`,
          )
          .first()
          .evaluate((element) =>
            parseFloat(getComputedStyle(element).animationDuration),
          );
      expect(await period(measured)).toBeLessThan(await period("idle"));
    }
    await expect(forward).toHaveCSS("animation-name", "app-topology-flow");
    await expect(reverse).toHaveCSS("animation-direction", "reverse");
    const initial = await forward.evaluate(
      (element) => getComputedStyle(element).strokeDashoffset,
    );
    await expect
      .poll(() =>
        forward.evaluate(
          (element) => getComputedStyle(element).strokeDashoffset,
        ),
      )
      .not.toBe(initial);
    await page.getByRole("button", { name: "Pause animation" }).click();
    await expect(forward).toHaveCSS("animation-play-state", "paused");
    expect(
      await flows.evaluateAll((elements) =>
        elements.every(
          (element) =>
            getComputedStyle(element).animationPlayState === "paused",
        ),
      ),
    ).toBe(true);
    const frozen = await forward.evaluate(async (element) => {
      await new Promise(requestAnimationFrame);
      const before = getComputedStyle(element).strokeDashoffset;
      await new Promise(requestAnimationFrame);
      return before === getComputedStyle(element).strokeDashoffset;
    });
    expect(frozen).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`${view.name}-streams.png`),
      fullPage: true,
    });
    await page.getByRole("button", { name: "Resume animation" }).click();
    await expect(forward).toHaveCSS("animation-play-state", "running");
    await page.emulateMedia({ reducedMotion: "reduce" });
    await expect(forward).toHaveCSS("animation-name", "none");
    await expect(reverse).toHaveCSS("animation-name", "none");
    // The lines stay: only the motion goes.
    expect(
      await page
        .locator(`${view.edges} .app-topology-edge__line`)
        .evaluateAll((elements) =>
          elements.every(
            (element) => Number(getComputedStyle(element).strokeOpacity) > 0,
          ),
        ),
    ).toBe(true);
  });
