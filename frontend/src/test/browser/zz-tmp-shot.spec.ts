import { test } from "@playwright/test";

test("tmp storage screenshot", async ({ page }) => {
  await page.setViewportSize({ width: 1600, height: 900 });
  await page.goto("/storage");
  await page.getByRole("button", { name: "Volume /srv/media" }).waitFor();
  await page.screenshot({
    path: "/tmp/claude-1000/-home-miguelmariz-LinuxIO/9fc546e1-a48c-4ae1-8790-79e5f7526aaf/scratchpad/storage.png",
    fullPage: true,
  });
});
