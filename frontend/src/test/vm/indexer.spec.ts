import { expect, test, type Page } from "@playwright/test";

import { bridgeCall, signIn, signOutIfSignedIn, vmSsh } from "./vm";

const SCAN_UNIT = "linuxio-indexer-index.service";
// Each run has a folder of its own: the daemon does not read again a folder
// it refreshed in the last 30 seconds, which a repeated run would be inside.
const FOLDERS = "/srv/e2e-indexer";

const scanUnit = async (property: string) =>
  (
    await vmSsh(`systemctl show --value --property=${property} ${SCAN_UNIT}`)
  ).trim();

/** What a search of the index for name answers, as text. */
const searchFor = async (page: Page, name: string) =>
  JSON.stringify(await bridgeCall(page, "filebrowser.search", { query: name }));

test.afterEach(async ({ page }) => {
  await signOutIfSignedIn(page);
  await vmSsh(`sudo rm -rf ${FOLDERS}`);
});

test("Index filesystem runs the scan unit, and a folder changed outside LinuxIO is found once it is opened", async ({
  page,
}) => {
  test.setTimeout(240_000);
  const FOLDER = `${FOLDERS}/${Date.now()}`;
  await vmSsh(
    `sudo mkdir -p ${FOLDER} && sudo chmod 755 ${FOLDERS} ${FOLDER} && sudo touch ${FOLDER}/before-the-scan.txt`,
  );
  await signIn(page);
  await page.goto("/filebrowser/srv");
  const index = page.getByRole("button", { name: "Index filesystem" });
  await expect(index).toBeVisible({ timeout: 15_000 });
  const lastRun = await scanUnit("InvocationID");

  // The page asks the bridge, the bridge asks systemd for the scanner, and
  // the scanner takes its turn with the daemon: the unit's own result is the
  // scan's.
  await index.click();
  await expect
    .poll(() => scanUnit("InvocationID"), { timeout: 60_000 })
    .not.toBe(lastRun);
  await expect
    .poll(() => scanUnit("ActiveState"), { timeout: 300_000 })
    .toBe("inactive");
  expect(await scanUnit("Result")).toBe("success");
  expect(await scanUnit("Nice")).toBe("19");
  // The task of the page ends with it.
  await expect(page.getByText("Indexing...")).toHaveCount(0, {
    timeout: 30_000,
  });
  await expect
    .poll(() => searchFor(page, "before-the-scan"), { timeout: 30_000 })
    .toContain(`${FOLDER}/before-the-scan.txt`);

  // A file made outside LinuxIO is unknown to the index until its folder is
  // opened, which refreshes that folder.
  await vmSsh(`sudo touch ${FOLDER}/made-outside.txt`);
  expect(await searchFor(page, "made-outside")).not.toContain(
    "made-outside.txt",
  );
  await page.goto(`/filebrowser${FOLDER}`);
  await expect(page.getByText("made-outside.txt").first()).toBeVisible({
    timeout: 15_000,
  });
  await expect
    .poll(() => searchFor(page, "made-outside"), { timeout: 30_000 })
    .toContain(`${FOLDER}/made-outside.txt`);

  // A rename through LinuxIO renames the rows: the file is found at its new
  // path and no longer at the old one.
  await page.getByText("made-outside.txt").first().click();
  await page.keyboard.press("F2");
  const name = page.locator('input[value="made-outside.txt"]');
  await expect(name).toBeVisible({ timeout: 15_000 });
  await name.fill("renamed-inside.txt");
  // To the page: the field no longer has the value it was found by.
  await page.keyboard.press("Enter");
  await expect(page.getByText("renamed-inside.txt").first()).toBeVisible({
    timeout: 15_000,
  });
  await expect
    .poll(() => searchFor(page, "renamed-inside"), { timeout: 30_000 })
    .toContain(`${FOLDER}/renamed-inside.txt`);
  expect(await searchFor(page, "made-outside")).not.toContain(
    "made-outside.txt",
  );
});
