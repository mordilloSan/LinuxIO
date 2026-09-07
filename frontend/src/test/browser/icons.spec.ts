import { expect, test } from "@playwright/test";

test("renders bundled distro and WireGuard icons without external requests", async ({
  page,
}) => {
  const external: string[] = [];
  await page.route("**/*", async (route) => {
    if (new URL(route.request().url()).hostname !== "127.0.0.1") {
      external.push(route.request().url());
      await route.abort();
    } else {
      await route.continue();
    }
  });
  await page.goto("/icons");
  await expect(page.getByRole("img")).toHaveCount(24);
  for (const icon of await page.getByRole("img").all()) {
    await expect(icon.locator("path")).not.toHaveCount(0);
    await expect(icon).toBeVisible();
  }
  expect(external).toEqual([]);
});
