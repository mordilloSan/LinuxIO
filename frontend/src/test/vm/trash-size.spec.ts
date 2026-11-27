import { expect, test, type Page } from "@playwright/test";

import { bridgeCall, signIn, signOutIfSignedIn, vmSsh } from "./vm";

// What the Trash takes on disk is read from the index, which only a real
// host has: a folder moved to the Trash is said to stay on the disk, the
// Trash button and the Trash then say how much, and an emptied Trash takes
// nothing. The unit tests answer for the indexer; here it is the daemon.

const SCAN_UNIT = "linuxio-indexer-index.service";
const MiB = 1024 * 1024;
// In the test user's home, so the folder goes to the home trash.
const FOLDER = "e2e-trash-size";

interface Usage {
  partial: boolean;
  pending: boolean;
  size: number;
}

const scanState = async () =>
  (
    await vmSsh(`systemctl show --value --property=ActiveState ${SCAN_UNIT}`)
  ).trim();

/** What filebrowser.trash_usage answers once the index has caught up. */
const settledUsage = async (page: Page): Promise<Usage> => {
  const answer = await bridgeCall(page, "filebrowser.trash_usage", null);
  expect(answer.status, JSON.stringify(answer)).toBe("ok");
  const usage = answer.data as Usage;
  // Still catching up: not an answer to compare yet.
  return usage.pending ? { ...usage, size: -1 } : usage;
};

test.afterEach(async ({ page }) => {
  await signOutIfSignedIn(page);
  await vmSsh(`rm -rf ~/${FOLDER}`);
});

test("a folder moved to the Trash is said to stay on the disk, until the Trash is emptied", async ({
  page,
}) => {
  test.setTimeout(480_000);
  const name = `build-${Date.now()}`;
  const home = (
    await vmSsh(
      `mkdir -p ~/${FOLDER}/${name} && dd if=/dev/urandom of=~/${FOLDER}/${name}/cache.bin bs=1M count=8 status=none && printf %s "$HOME"`,
    )
  ).trim();
  const folder = `${home}/${FOLDER}`;

  await signIn(page);
  await page.goto(`/filebrowser${folder}`);
  await expect(page.getByText(name).first()).toBeVisible({ timeout: 15_000 });

  // The sizes are the index's, which a full index fills.
  const index = page.getByRole("button", { name: "Index filesystem" });
  await expect(index).toBeVisible({ timeout: 15_000 });
  await index.click();
  await expect.poll(scanState, { timeout: 60_000 }).not.toBe("inactive");
  await expect.poll(scanState, { timeout: 300_000 }).toBe("inactive");
  await expect(page.getByText("Indexing...")).toHaveCount(0, {
    timeout: 30_000,
  });
  const before = await (async () => {
    let usage: Usage = { partial: false, pending: true, size: -1 };
    await expect
      .poll(async () => (usage = await settledUsage(page)).size, {
        timeout: 60_000,
      })
      .toBeGreaterThanOrEqual(0);
    return usage.size;
  })();
  // The folder's size is in the listing before the delete is asked for.
  await page.reload();
  await expect
    .poll(
      async () => {
        const answer = await bridgeCall(page, "filebrowser.subfolders", {
          path: folder,
        });
        const data = answer.data as
          | { pending: boolean; subfolders: { path: string; size: number }[] }
          | undefined;
        const sized = data?.subfolders.find(
          (item) => item.path === `${folder}/${name}`,
        );
        return data?.pending ? 0 : (sized?.size ?? 0);
      },
      { timeout: 60_000 },
    )
    .toBeGreaterThanOrEqual(8 * MiB);
  await expect(page.getByText(name).first()).toBeVisible({ timeout: 15_000 });

  // The question says what stays on the disk.
  await page.getByText(name).first().click();
  await page.keyboard.press("Delete");
  const question = page.getByRole("dialog", { name: "Move to Trash" });
  await expect(question).toContainText(
    /It stays on the disk \(8(\.\d)? MB\) until the Trash is emptied\./,
    { timeout: 15_000 },
  );
  await expect(
    question.getByRole("button", { name: "Delete permanently instead" }),
  ).toBeVisible();
  await question.getByRole("button", { name: "Move to Trash" }).click();
  await expect(
    page
      .locator("[data-sonner-toast]")
      .filter({ hasText: /Moved 1 item to Trash, 8(\.\d)? MB still used/ }),
  ).toBeVisible({ timeout: 30_000 });

  // The route, then the button that shows its answer in every folder.
  await expect
    .poll(async () => (await settledUsage(page)).size - before, {
      timeout: 60_000,
    })
    .toBeGreaterThanOrEqual(8 * MiB);
  expect((await settledUsage(page)).partial).toBe(false);
  const trash = page.getByRole("button", { name: /^Trash, .*(MB|GB)$/ });
  await expect(trash).toBeVisible({ timeout: 30_000 });

  // In the Trash, the folder has its size and the header the total.
  await trash.click();
  const list = page.getByRole("listbox", { name: "Trash" });
  await expect(
    list.getByRole("option").filter({ hasText: name }),
  ).toContainText(/8(\.\d)? MB/, { timeout: 30_000 });
  await expect(page.getByText(/^\d+ items?, .*(MB|GB)$/)).toBeVisible({
    timeout: 30_000,
  });

  // Emptied, the Trash takes nothing and its button says no size.
  await page.getByRole("button", { name: "Empty Trash" }).first().click();
  const empty = page.getByRole("dialog", { name: "Empty Trash" });
  await empty.getByRole("button", { name: "Empty Trash" }).click();
  await expect(page.getByText("Trash is empty.")).toBeVisible({
    timeout: 60_000,
  });
  await expect
    .poll(async () => (await settledUsage(page)).size, { timeout: 60_000 })
    .toBe(0);
  await page.goto(`/filebrowser${folder}`);
  await expect(
    page.getByRole("button", { name: "Trash", exact: true }),
  ).toBeVisible({ timeout: 30_000 });
});
