import { expect, test } from "@playwright/test";

for (const view of ["docker", "storage"] as const) {
  test(`${view} grains move, pause in place, and respect reduced motion`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 1100 });
    await page.emulateMedia({ reducedMotion: "no-preference" });
    await page.goto(`/${view}/topology`);
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
    // A horizontal SVG path has zero bounding-box height even with a visible stroke.
    await expect(page.locator(`.${view}-topology__edges`)).toBeVisible();
    // Inspect every track: the faint edges and varied motion form one stream.
    const texture = await grain
      .locator("..")
      .locator(".app-topology-edge__grain")
      .evaluateAll((elements) =>
        elements.map((element) => {
          const style = getComputedStyle(element);
          return {
            opacity: Number(style.strokeOpacity),
            width: parseFloat(style.strokeWidth),
            transform: style.transform,
            duration: style.animationDuration,
            spacing: style.strokeDasharray,
          };
        }),
      );
    expect(texture.length).toBeGreaterThan(2);
    expect(Math.max(...texture.map((track) => track.width))).toBeLessThan(1);
    expect(texture[0].opacity).toBeLessThan(
      texture[Math.floor(texture.length / 2)].opacity,
    );
    for (const property of ["transform", "duration", "spacing"] as const)
      expect(
        new Set(texture.map((track) => track[property])).size,
      ).toBeGreaterThan(1);
    await expect(grain).toHaveCSS("animation-name", "app-topology-flow");
    await expect(reverse).toHaveCSS("animation-direction", "reverse");
    const initial = await grain.evaluate(
      (element) => getComputedStyle(element).strokeDashoffset,
    );
    await expect
      .poll(() =>
        grain.evaluate((element) => getComputedStyle(element).strokeDashoffset),
      )
      .not.toBe(initial);
    await page.getByRole("button", { name: "Pause animation" }).click();
    await expect(grain).toHaveCSS("animation-play-state", "paused");
    await expect(reverse).toHaveCSS("animation-play-state", "paused");
    expect(
      await page
        .locator(".app-topology-edge__grain")
        .evaluateAll((elements) =>
          elements.every(
            (element) =>
              getComputedStyle(element).animationPlayState === "paused",
          ),
        ),
    ).toBe(true);
    const frozen = await grain.evaluate(async (element) => {
      await new Promise(requestAnimationFrame);
      const before = getComputedStyle(element).strokeDashoffset;
      await new Promise(requestAnimationFrame);
      return before === getComputedStyle(element).strokeDashoffset;
    });
    expect(frozen).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`${view}-grains.png`),
      fullPage: true,
    });
    await page.getByRole("button", { name: "Resume animation" }).click();
    await expect(grain).toHaveCSS("animation-play-state", "running");
    await page.emulateMedia({ reducedMotion: "reduce" });
    await expect(grain).toHaveCSS("animation-name", "none");
    await expect(reverse).toHaveCSS("animation-name", "none");
    expect(
      await page
        .locator(".app-topology-edge__grain")
        .evaluateAll((elements) =>
          elements.every(
            (element) =>
              getComputedStyle(element).animationName === "none" &&
              Number(getComputedStyle(element).strokeOpacity) > 0,
          ),
        ),
    ).toBe(true);
    await expect(
      page.getByText("Live activity", { exact: true }),
    ).toBeVisible();
  });
}
