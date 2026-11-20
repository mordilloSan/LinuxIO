import { expect, type Page, test } from "@playwright/test";

// The fixture is the dashboard's Memory card on a host that swaps to its
// swap file, with an /etc/default/zramswap the admin wrote
// (fixture/routes/MemoryOverflowPage.tsx). It has no bridge: nothing here
// applies a change.

const dialog = (page: Page) =>
  page.getByRole("dialog", { name: "Memory overflow" });
const radio = (page: Page, name: string) =>
  page.getByRole("radio", { exact: true, name });

const openFromPencil = async (page: Page) => {
  await page.goto("/dashboard/memory");
  await page.getByRole("button", { name: "Edit Swap" }).click();
  await expect(dialog(page)).toBeVisible();
};

test("the Swap row's pencil opens the dialog into the URL, and Escape takes it out", async ({
  page,
}) => {
  await openFromPencil(page);
  await expect(page).toHaveURL(/\/dashboard\/memory\?dialog=memory-overflow$/);

  await page.keyboard.press("Escape");
  await expect(dialog(page)).toBeHidden();
  await expect(page).toHaveURL(/\/dashboard\/memory$/);
});

test("a reload with the dialog in the URL opens it", async ({ page }) => {
  await openFromPencil(page);
  await radio(page, "Compressed memory only").click();

  await page.reload();

  await expect(dialog(page)).toBeVisible();
  // An unapplied choice does not survive the reload; the host's mode does.
  await expect(radio(page, "Disk only")).toBeChecked();
  await expect(radio(page, "Compressed memory only")).not.toBeChecked();
});

test("the rows are one choice, and the eagerness slider follows it", async ({
  page,
}) => {
  await openFromPencil(page);
  const group = page.getByRole("radiogroup", { name: "When memory runs out" });
  await expect(group.getByRole("radio")).toHaveCount(3);
  await expect(radio(page, "Disk only")).toBeChecked();

  await radio(page, "Compressed memory only").click();
  await expect(radio(page, "Compressed memory only")).toBeChecked();
  await expect(radio(page, "Disk only")).not.toBeChecked();
  // The radios share one name, so the arrow keys move the one choice.
  await page.keyboard.press("ArrowUp");
  await expect(
    radio(page, "Compressed memory with the disk as backup"),
  ).toBeChecked();
  await expect(radio(page, "Compressed memory only")).not.toBeChecked();
  await page.keyboard.press("ArrowDown");
  await expect(radio(page, "Compressed memory only")).toBeChecked();

  await page.getByRole("button", { name: "Advanced" }).click();
  const slider = page.getByRole("slider", {
    name: "Eagerness to move data out of memory",
  });
  await expect(slider).toBeVisible();
  await expect(slider).toHaveValue("60");
  await expect(
    dialog(page).getByText("Recommended for Compressed memory only: 180"),
  ).toBeVisible();
  await radio(page, "Disk only").click();
  await expect(
    dialog(page).getByText("Recommended for Disk only: 60"),
  ).toBeVisible();

  // A key moves the slider at once and saves half a second later. With no
  // bridge the save fails, and the slider goes back to the host's value, as
  // after any failed save.
  await slider.focus();
  await page.keyboard.press("ArrowRight");
  await expect(slider).toHaveValue("61");
  await expect(slider).toHaveValue("60");
  await expect(slider).toBeFocused();
});

test("the confirm step names what it changes, opens on Back and returns to Apply", async ({
  page,
}) => {
  await openFromPencil(page);
  await radio(page, "Compressed memory only").click();
  await page.getByRole("button", { name: "Apply" }).click();

  const confirm = page.getByRole("dialog", {
    name: "Switch to compressed memory?",
  });
  await expect(confirm).toBeVisible();
  await expect(confirm).toContainText(
    "The swap file is turned off and stays off after a restart.",
  );
  await expect(confirm).toContainText(
    "/etc/default/zramswap holds zram settings that are not LinuxIO's. It is replaced, and a copy is kept as /etc/default/zramswap.before-linuxio.",
  );
  await expect(page.getByRole("button", { name: "Back" })).toBeFocused();
  await expect(
    page.getByRole("button", { name: "Turn on compressed memory" }),
  ).toBeEnabled();

  await page.keyboard.press("Enter");

  await expect(confirm).toBeHidden();
  await expect(dialog(page)).toBeVisible();
  await expect(page.getByRole("button", { name: "Apply" })).toBeFocused();
  await expect(radio(page, "Compressed memory only")).toBeChecked();
  await expect(page).toHaveURL(/\?dialog=memory-overflow$/);
});
