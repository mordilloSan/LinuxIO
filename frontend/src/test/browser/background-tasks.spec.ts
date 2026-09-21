import { expect, test } from "@playwright/test";

test("progress commits only its task subscriber, while removal updates membership", async ({
  page,
}) => {
  await page.goto("/background-tasks");
  const first = page.getByTestId("first");
  const sibling = page.getByTestId("second");
  const list = page.getByTestId("list");
  const flag = page.getByTestId("indexing");
  await expect(first).toHaveText("0");
  await expect(list).toHaveText("first,second");
  const before = await Promise.all(
    [first, sibling, list, flag].map((item) =>
      item.getAttribute("data-commits"),
    ),
  );
  for (let progress = 10; progress <= 30; progress += 10) {
    await page.getByRole("button", { name: "Advance first task" }).click();
    await expect(first).toHaveText(String(progress));
  }
  const after = await Promise.all(
    [first, sibling, list, flag].map((item) =>
      item.getAttribute("data-commits"),
    ),
  );
  expect(Number(after[0])).toBeGreaterThan(Number(before[0]));
  expect(after.slice(1)).toEqual(before.slice(1));
  await page.getByRole("button", { name: "Remove first task" }).click();
  await expect(first).toHaveText("removed");
  await expect(list).toHaveText("second");
  await expect(sibling).toHaveAttribute("data-commits", before[1]!);
  await expect(flag).toHaveAttribute("data-commits", before[3]!);
});
