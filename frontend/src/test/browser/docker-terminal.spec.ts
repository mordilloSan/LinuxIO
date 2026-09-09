import { expect, test } from "@playwright/test";

test("types spaces in a container shell without dragging its card", async ({
  page,
}) => {
  let input = "";
  await page.routeWebSocket("**/ws", (socket) => {
    let terminalId: number | undefined;
    socket.onMessage((message) => {
      if (typeof message === "string") return;
      if (message[4] === 0x01) {
        const { route } = JSON.parse(message.subarray(14).toString());
        if (route === "container.open") terminalId = message.readUInt32BE(0);
      } else if (
        message.readUInt32BE(0) === terminalId &&
        message[5] === 0x81
      ) {
        input += message.subarray(14).toString();
        const echo = Buffer.from(message);
        echo[4] = 0x04;
        socket.send(echo);
      }
    });
  });

  await page.goto("/docker/container-actions");
  await page
    .getByRole("button", { name: "Actions for example", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "Terminal", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.locator(".xterm-helper-textarea")).toBeFocused();
  await page.keyboard.type("echo hello  world ");
  await page.keyboard.press("Enter");
  await expect.poll(() => input).toBe("echo hello  world \r");
  await expect(page.locator(".sc-drag-overlay")).toHaveCount(0);

  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await page.locator(".sortable-card").focus();
  await page.keyboard.press("Space");
  await expect(page.locator(".sc-drag-overlay")).toBeVisible();
  await page.keyboard.press("Space");
  await expect(page.locator(".sc-drag-overlay")).toHaveCount(0);
});
