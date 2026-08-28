import { expect, test } from "@playwright/test";

/**
 * The unit suite runs in jsdom, which does not compute stylesheet CSS, so a
 * token or component change that alters what renders passes it silently. The
 * styling gallery fixture draws the type scale, palette, spacing and the
 * shared components in each colour scheme; these screenshots pin what they
 * look like. After a deliberate visual change, `make
 * update-frontend-screenshots` rewrites the baselines and the PNG diff is
 * reviewed in git like any other change.
 */
for (const scheme of ["dark", "light"] as const) {
  test(`styling gallery renders the ${scheme} scheme as recorded`, async ({
    page,
  }) => {
    await page.goto(`/styling/${scheme}`);
    await page.getByTestId("styling-gallery").waitFor();
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.dataset.appColorScheme),
      )
      .toBe(scheme);
    await page.evaluate(() => document.fonts.ready);

    await expect(page).toHaveScreenshot(`styling-gallery-${scheme}.png`, {
      animations: "disabled",
      fullPage: true,
    });
  });
}
