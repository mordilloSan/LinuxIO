import { expect, test } from "@playwright/test";

import { signIn, signOut } from "./vm";

test("signs in, sees the dashboard and signs out", async ({ page }) => {
  await signIn(page);
  await expect(page.getByText("System Overview")).toBeVisible({
    timeout: 15_000,
  });
  await signOut(page);
});
