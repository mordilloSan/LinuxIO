import { expect, test } from "@playwright/test";

test("creates a script draft with multiline arguments and opens unit logs", async ({
  page,
}) => {
  await page.goto("/schedules");
  await page.getByRole("button", { name: "Create task" }).click();
  await page.getByLabel("Arguments (one per line)").fill("--full\n--quiet");
  await expect(page.getByLabel("Arguments (one per line)")).toHaveValue(
    "--full\n--quiet",
  );
  await page.getByRole("button", { name: "Cancel" }).click();
  await page.getByRole("button", { name: "Logs", exact: true }).click();
  await expect(page).toHaveURL(
    /\/logs\?unit=linuxio-schedule-nightly\.service$/,
  );
});

test("chooses Saturday at 7am and a working directory with shared dialog typography", async ({
  page,
}, testInfo) => {
  let saved: Record<string, unknown> | undefined;
  await page.routeWebSocket("**/ws", (socket) => {
    socket.onMessage((message) => {
      if (typeof message === "string" || message[4] !== 0x01) return;
      const { route, request } = JSON.parse(message.subarray(14).toString());
      let data: unknown = null;
      switch (route) {
        case "filebrowser.directory_children":
          data = { folders: ["backups"], files: [] };
          break;
        case "schedules.create":
          saved = request.options;
          break;
        case "schedules.list":
          data = { available: true, error: null, schedules: [] };
          break;
        case "schedules.get":
          break;
        case "accounts.list_users":
          data = [{ username: "root" }, { username: "alice" }];
          break;
        default:
          return;
      }
      const body = Buffer.from(JSON.stringify({ status: "ok", data }));
      const frame = Buffer.alloc(14 + body.length);
      const id = message.readUInt32BE(0);
      frame.writeUInt32BE(id, 0);
      frame[4] = 0x04;
      frame[5] = 0x85;
      frame.writeUInt32BE(id, 6);
      frame.writeUInt32BE(body.length, 10);
      body.copy(frame, 14);
      socket.send(frame);
    });
  });
  await page.goto("/schedules");
  await page.getByRole("button", { name: "Create task" }).click();
  const dialog = page.getByRole("dialog", { name: "Create scheduled task" });
  await expect(dialog.getByLabel("Time", { exact: true })).toHaveValue("03:00");
  await dialog.getByLabel("Name", { exact: true }).fill("Weekly backup");
  await dialog.getByLabel("Run as").click();
  await page.getByRole("option", { name: "root", exact: true }).click();
  await dialog.getByLabel("Repeat").click();
  await page.getByRole("option", { name: "Weekly", exact: true }).click();
  await dialog.getByLabel("Day of the week").click();
  await page.getByRole("option", { name: "Saturday", exact: true }).click();
  await dialog.getByLabel("Time", { exact: true }).fill("07:00");
  await expect(dialog.getByText(/Every Saturday at /)).toBeVisible();
  await dialog
    .getByRole("button", { name: "Browse working directory" })
    .click();
  await page.getByRole("button", { name: "Expand /", exact: true }).click();
  await page.getByText("backups", { exact: true }).click();
  await expect(
    dialog.getByLabel("Working directory", { exact: true }),
  ).toHaveValue("/backups/");
  await expect(dialog.locator(".app-dialog-title")).toHaveCSS(
    "font-size",
    "16px",
  );
  await expect(dialog.getByLabel("Name", { exact: true })).toHaveCSS(
    "font-size",
    "13px",
  );
  await expect(dialog.getByLabel("Repeat")).toHaveCSS("font-size", "13px");
  await page.evaluate(() => document.fonts.ready);
  await testInfo.attach("schedule-dialog", {
    body: await dialog.screenshot({
      path: testInfo.outputPath("schedule-dialog.png"),
    }),
    contentType: "image/png",
  });
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect
    .poll(() => saved)
    .toMatchObject({
      name: "Weekly backup",
      on_calendar: "Sat *-*-* 07:00:00",
      working_directory: "/backups/",
    });
  await expect(dialog).not.toBeVisible();
});
