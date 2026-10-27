import { expect, test } from "@playwright/test";

// Opt-in check against an installed LinuxIO (`make localinstall`) with Docker,
// not the browser fixture: it really updates LINUXIO_E2E_CONTAINER, a running
// container with an update available whose update takes a few seconds.
const enabled = process.env.LINUXIO_E2E_DOCKER === "1";
const container = process.env.LINUXIO_E2E_CONTAINER ?? "";
const username = process.env.LINUXIO_E2E_USER ?? "";
const password = process.env.LINUXIO_E2E_PASSWORD ?? "";

test.describe("jobs", () => {
  test.skip(
    !enabled,
    "set LINUXIO_E2E_DOCKER=1 to run against an installed LinuxIO with Docker",
  );
  test.use({
    baseURL: process.env.LINUXIO_E2E_URL ?? "https://localhost:8090",
    ignoreHTTPSErrors: true,
  });

  test("starts a container update, reloads, and finds the job in the tray", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    expect(container, "LINUXIO_E2E_CONTAINER").not.toBe("");
    expect(username, "LINUXIO_E2E_USER").not.toBe("");

    await page.goto("/sign-in");
    await page.locator('input[autocomplete="username"]').fill(username);
    await page.locator('input[autocomplete="current-password"]').fill(password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page).not.toHaveURL(/\/sign-in/);

    // The containers page shows cards or a table, by the user's preference.
    await page.goto("/docker/containers");
    const card = page.getByRole("button", {
      name: `Update available for ${container}`,
    });
    const row = page
      .getByRole("row")
      .filter({ hasText: container })
      .getByRole("button", { name: "Update", exact: true });
    await expect(card.or(row)).toBeVisible();
    if (await card.isVisible()) {
      await card.click();
      await page.getByRole("menuitem", { name: "Update" }).click();
    } else {
      await row.click();
    }
    await expect(page.getByText(`Updating ${container}`)).toBeVisible();

    // A reload drops the mutation stream; the job runs on in the job host.
    // The Docker page reopens its dialog for the recovered job: close it.
    await page.reload();
    const close = page.getByRole("button", {
      name: "Close container update dialog",
    });
    await expect(close).toBeVisible({ timeout: 15_000 });
    await close.click();

    await page
      .getByRole("button", { name: "Notifications", exact: true })
      .click();
    await expect(
      page
        .getByRole("dialog", { name: "Notifications" })
        .getByRole("listitem")
        .filter({ hasText: /docker|container|image|compose|update/i }),
    ).toBeVisible();
  });
});
