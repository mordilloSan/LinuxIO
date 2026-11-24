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

/** A search below basePath, which also reads that folder on disk. */
const searchIn = (page: Page, basePath: string, name: string) =>
  bridgeCall(page, "filebrowser.search", { basePath, query: name });

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

test("an open folder shows a file made or deleted outside LinuxIO without a reload", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const FOLDER = `${FOLDERS}/live-${Date.now()}`;
  await vmSsh(
    `sudo mkdir -p ${FOLDER} && sudo chmod 755 ${FOLDERS} ${FOLDER} && sudo touch ${FOLDER}/listed-before.txt`,
  );
  await signIn(page);
  await page.goto(`/filebrowser${FOLDER}`);
  await expect(page.getByText("listed-before.txt").first()).toBeVisible({
    timeout: 15_000,
  });
  // The page opens its watch of the folder after the listing was read and
  // shown, and the bridge puts it on the folder a moment later: a change in
  // between is not reported (a known limit), so the test waits that out.
  await page.waitForTimeout(1000);
  const shownAt = page.url();
  let navigations = 0;
  page.on("framenavigated", (frame) => {
    if (frame === page.mainFrame()) navigations += 1;
  });

  await vmSsh(`sudo touch ${FOLDER}/made-while-open.txt`);
  await expect(page.getByText("made-while-open.txt").first()).toBeVisible({
    timeout: 10_000,
  });
  await vmSsh(`sudo rm ${FOLDER}/listed-before.txt`);
  await expect(page.getByText("listed-before.txt")).toHaveCount(0, {
    timeout: 10_000,
  });

  // Nothing read the folder again on the page's side: no reload, no
  // navigation.
  expect(page.url()).toBe(shownAt);
  expect(navigations).toBe(0);
});

test("a search inside a folder finds a file made outside LinuxIO in a subfolder never opened", async ({
  page,
}) => {
  test.setTimeout(240_000);
  const FOLDER = `${FOLDERS}/walk-${Date.now()}`;
  await vmSsh(`sudo mkdir -p ${FOLDER} && sudo chmod 755 ${FOLDERS} ${FOLDER}`);
  await signIn(page);
  // Search answers once a full index has completed, and reads the disk
  // beside it: a VM that never scanned scans first.
  if ((await searchIn(page, "/", "any-name")).status !== "ok") {
    await vmSsh(`sudo systemctl start ${SCAN_UNIT}`);
  }
  await vmSsh(
    `sudo mkdir -p ${FOLDER}/never/opened && sudo touch ${FOLDER}/never/opened/found-on-disk.txt`,
  );

  // The index has not seen the file: a search from / reads the index only.
  expect(await searchFor(page, "found-on-disk")).not.toContain(
    "found-on-disk.txt",
  );
  // A search from a folder above it reads that folder on disk.
  const below = await searchIn(page, FOLDERS, "found-on-disk");
  expect(below.status).toBe("ok");
  expect(below.data).toMatchObject({
    results: [
      expect.objectContaining({
        path: `${FOLDER}/never/opened/found-on-disk.txt`,
      }),
    ],
    walk: "complete",
  });

  // The page searches the folder it shows.
  await page.goto(`/filebrowser${FOLDER}`);
  await page
    .getByPlaceholder("Search files and folders...")
    .fill("found-on-disk");
  await expect(page.getByText("found-on-disk.txt").first()).toBeVisible({
    timeout: 15_000,
  });
});
