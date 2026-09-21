import { expect, test } from "@playwright/test";

for (const view of ["docker", "storage"] as const) {
  test(`${view} topology clears every node selection with Escape`, async ({
    page,
  }) => {
    await page.goto(`/${view}/topology`);
    const nodes = page.getByRole("button", { name: /^Inspect / });
    await expect(nodes.first()).toBeVisible();

    for (const node of await nodes.all()) {
      await node.click();
      await expect(node).toHaveAttribute("aria-pressed", "true");
      await expect(page).toHaveURL(/\?/);
      await page.keyboard.press("Escape");
      await expect(node).toHaveAttribute("aria-pressed", "false");
      await expect(page).toHaveURL(new RegExp(`/${view}/topology$`));
      await expect(
        page.locator('.app-topology-edge[data-highlighted="true"]'),
      ).toHaveCount(0);
      await expect(
        page.getByRole("button", { name: "Clear selection" }),
      ).toHaveCount(0);
    }

    // Clearing follows the same URL navigation path as the clear button.
    await page.goBack();
    await expect(page).toHaveURL(/\?/);
    await expect(nodes.last()).toHaveAttribute("aria-pressed", "true");
    await page.goForward();
    await expect(page).toHaveURL(new RegExp(`/${view}/topology$`));
    await page.reload();
    await expect(nodes.last()).toHaveAttribute("aria-pressed", "false");
  });
}
