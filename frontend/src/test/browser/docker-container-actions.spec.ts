import { expect, test } from "@playwright/test";

test("keeps destructive container actions keyboard-safe and explicit", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1600, height: 1000 });
  await page.goto("/docker/container-actions");
  await expect(
    page.getByRole("heading", { name: "Container lifecycle fixture" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Open container details" }).click();
  await expect(page.getByText("Environment variables")).toBeVisible();
  await expect(page.getByText("••••••••").first()).toBeVisible();
  const configurationCard = page
    .getByText("Configuration", { exact: true })
    .locator("xpath=ancestor::*[contains(@class, 'frosted-card')][1]");
  const environmentCard = page
    .getByText("Environment variables", { exact: true })
    .locator("xpath=ancestor::*[contains(@class, 'frosted-card')][1]");
  const overviewCard = page
    .getByText("Overview and health", { exact: true })
    .locator("xpath=ancestor::*[contains(@class, 'frosted-card')][1]");
  const labelsCard = page
    .getByText("Labels", { exact: true })
    .locator("xpath=ancestor::*[contains(@class, 'frosted-card')][1]");
  const portsCard = page
    .getByText("Ports", { exact: true })
    .locator("xpath=ancestor::*[contains(@class, 'frosted-card')][1]");
  await expect(overviewCard).not.toContainText("Monitoring");
  await expect(labelsCard).not.toContainText("Ports");
  await expect(portsCard).toContainText("Mounts");
  await expect(portsCard).toContainText("Networks");
  const configurationBox = await configurationCard.boundingBox();
  const environmentBox = await environmentCard.boundingBox();
  const labelsBox = await labelsCard.boundingBox();
  expect(configurationBox).not.toBeNull();
  expect(environmentBox).not.toBeNull();
  expect(labelsBox).not.toBeNull();
  expect(Math.abs(configurationBox!.y - environmentBox!.y)).toBeLessThan(2);
  expect(configurationBox!.x).toBeLessThan(environmentBox!.x);
  expect(
    Math.abs(configurationBox!.height - environmentBox!.height),
  ).toBeLessThan(2);
  expect(Math.abs(configurationBox!.height - labelsBox!.height)).toBeLessThan(
    2,
  );
  expect(
    await environmentCard.evaluate(
      (element) => element.scrollHeight > element.clientHeight,
    ),
  ).toBe(true);
  expect(
    await labelsCard.evaluate(
      (element) => element.scrollHeight > element.clientHeight,
    ),
  ).toBe(true);
  const firstLabelBox = await labelsCard
    .getByText("label.0=value-0")
    .boundingBox();
  const secondLabelBox = await labelsCard
    .getByText("label.1=value-1")
    .boundingBox();
  expect(firstLabelBox).not.toBeNull();
  expect(secondLabelBox).not.toBeNull();
  expect(Math.abs(firstLabelBox!.x - secondLabelBox!.x)).toBeLessThan(2);
  expect(firstLabelBox!.y).toBeLessThan(secondLabelBox!.y);

  // A 640 CSS-pixel viewport exercises the layout seen at 200% zoom on a
  // typical 1280px desktop viewport.
  await page.setViewportSize({ width: 640, height: 900 });
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await expect(
    page.getByText("/var/run/docker.sock → /var/run/docker.sock (read-only)"),
  ).toBeVisible();
  await expect(page.getByText("bind", { exact: true })).toBeVisible();
  await expect(page.getByText("tcp", { exact: true })).toHaveCount(1);
  await expect(page.getByText("0.0.0.0:3001 → 3000")).toBeVisible();
  await expect(page.getByText(":::3001 → 3000")).toHaveCount(0);
  await portsCard
    .getByText("Networks", { exact: true })
    .scrollIntoViewIfNeeded();
  await expect(page.getByText("Custom network bridge")).toBeVisible();
  await expect(page.getByText("Teste_Default")).toBeVisible();
  await expect(page.getByText("IPv4", { exact: true })).toBeVisible();
  await expect(page.getByText("172.20.0.2")).toBeVisible();
  await expect(page.getByText("IPv6", { exact: true })).toBeVisible();
  await expect(page.getByText("2001:db8::2")).toBeVisible();
  await expect
    .poll(async () => {
      const label = await page
        .getByText("HOMEPAGE_ALLOWED_HOSTS")
        .boundingBox();
      const value = await page.getByText("••••••••").first().boundingBox();
      return Boolean(
        label &&
        value &&
        label.x + label.width <= value.x &&
        Math.abs(label.y - value.y) < 2,
      );
    })
    .toBe(true);

  await page.getByRole("button", { name: "Actions for example" }).click();
  await page.getByRole("menuitem", { name: "Kill" }).click();
  const killDialog = page.getByRole("dialog", { name: "Kill example?" });
  await expect(killDialog).toBeVisible();
  await expect(killDialog).toContainText("SIGKILL");
  await page.keyboard.press("Escape");
  await expect(killDialog).toBeHidden();

  await page.getByRole("button", { name: "Actions for example" }).click();
  await page.getByRole("menuitem", { name: "Remove" }).click();
  const removeDialog = page.getByRole("dialog", { name: "Remove example?" });
  const confirm = removeDialog.getByRole("button", {
    name: "Remove container",
  });
  await expect(confirm).toBeDisabled();
  await removeDialog
    .getByRole("checkbox", {
      name: "Force removal of this active container",
    })
    .check();
  await expect(confirm).toBeEnabled();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Close container details" }).click();
  await expect(
    page.getByRole("button", { name: "Open container details" }),
  ).toBeVisible();
});
