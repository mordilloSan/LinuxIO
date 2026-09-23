import { expect, test, type Page } from "@playwright/test";

import type {
  RsyncSSHConfig,
  RsyncSSHSaveRequest,
  RsyncSaveRequest,
  RsyncStatus,
} from "@/api";

async function mockRsyncServer(
  page: Page,
  initialStatus: RsyncStatus = { active: false, enabled: false },
) {
  let status = initialStatus;
  const saves: RsyncSaveRequest[] = [];
  const sshSaves: RsyncSSHSaveRequest[] = [];
  const sshPorts: number[] = [];
  let sshConfig: RsyncSSHConfig | undefined;
  await page.routeWebSocket("**/ws", (socket) => {
    socket.onMessage((message) => {
      if (typeof message === "string" || message[4] !== 0x01) return;
      const id = message.readUInt32BE(0);
      const { route, request } = JSON.parse(message.subarray(14).toString());
      if (route === "shares.save_rsync") {
        saves.push(request);
        const { password: _password, ...config } = request;
        status = { config, active: true, enabled: true };
      } else if (route === "shares.stop_rsync") {
        status = { ...status, active: false, enabled: false };
      }
      if (route === "shares.get_rsync_ssh") sshPorts.push(request.port);
      if (route === "shares.save_rsync_ssh") {
        sshSaves.push(request);
        sshConfig = { ...request, module: "backup" };
      } else if (route === "shares.remove_rsync_ssh") {
        sshConfig = undefined;
      }
      const data =
        route === "shares.get_rsync_ssh"
          ? { available: true, port: request.port, config: sshConfig }
          : route === "shares.save_rsync_ssh"
            ? sshConfig
            : route === "shares.remove_rsync_ssh"
              ? { success: true }
              : status;
      const body = Buffer.from(JSON.stringify({ data, status: "ok" }));
      const frame = Buffer.alloc(14 + body.length);
      frame.writeUInt32BE(id, 0);
      frame[4] = 0x04;
      frame[5] = 0x85;
      frame.writeUInt32BE(id, 6);
      frame.writeUInt32BE(body.length, 10);
      body.copy(frame, 14);
      socket.send(frame);
    });
  });
  return { saves, sshSaves, sshPorts };
}

test("configures a read-only backup module without a terminal", async ({
  page,
}) => {
  const { saves, sshSaves, sshPorts } = await mockRsyncServer(page);
  await page.goto("/shares/rsync");
  const ssh = page.getByRole("region", { name: "SSH backup connection" });
  await expect(ssh.getByLabel("SSH port", { exact: false })).toHaveValue("22");
  await expect.poll(() => sshPorts).toContain(22);

  await ssh.getByLabel("SSH port", { exact: false }).fill("9222");
  await ssh.getByRole("button", { name: "Check SSH port 9222" }).click();
  await expect.poll(() => sshPorts).toContain(9222);
  await expect(ssh.getByRole("button", { name: "Remove" })).toBeDisabled();
  await ssh
    .getByLabel("Linux account for SSH", { exact: false })
    .fill("backup-user");
  await ssh
    .getByLabel("SSH source folder", { exact: false })
    .fill("/home/backup-user/data");
  await ssh.getByRole("button", { name: "Save SSH module" }).click();
  await expect
    .poll(() => sshSaves)
    .toEqual([{ username: "backup-user", path: "/home/backup-user/data" }]);
  await expect(ssh).toContainText("Connect from TOS");
  await expect(ssh).toContainText("9222");
  await expect(ssh).toContainText("backup-user");
  await expect(ssh).toContainText("/home/backup-user/data");
  await ssh.getByLabel("SSH port", { exact: false }).fill("65536");
  await expect(
    ssh.getByRole("button", { name: "Check SSH port 65536" }),
  ).toBeDisabled();
  expect(sshPorts).not.toContain(65536);
  expect(saves).toHaveLength(0);
  await page.getByLabel("Backup folder", { exact: false }).fill("/srv/data");
  await page
    .getByLabel("TNAS IP address", { exact: false })
    .fill("192.168.1.249");
  await page
    .getByLabel("Backup password", { exact: false })
    .fill("test-backup-password");
  await page.getByRole("button", { name: "Save and start" }).click();
  await expect(page.getByText("Running", { exact: true })).toBeVisible();
  expect(saves[0]).toMatchObject({
    module: "backup",
    path: "/srv/data",
    port: 873,
    nas_address: "192.168.1.249",
    username: "tnas",
  });
  await expect(
    page.getByLabel("Backup password", { exact: false }),
  ).toHaveValue("");
  await page.reload();
  await expect(
    page.getByRole("region", { name: "TOS connection details" }),
  ).toContainText("873");
  await expect(ssh).toContainText("backup-user");
  await ssh.getByRole("button", { name: "Remove" }).click();
  await expect(ssh).not.toContainText("Connect from TOS");
  await expect(ssh.getByRole("button", { name: "Remove" })).toBeDisabled();
  await page.getByLabel("Module port", { exact: false }).fill("8873");
  await page.getByRole("button", { name: "Save and start" }).click();
  await expect.poll(() => saves.length).toBe(2);
  expect(saves[1]).toMatchObject({ port: 8873, password: "" });
  await expect(
    page.getByRole("region", { name: "TOS connection details" }),
  ).toContainText("8873");
  await page.getByRole("button", { name: "Stop and disable" }).click();
  await expect(page.getByText("Stopped", { exact: true })).toBeVisible();
});

for (const scheme of ["dark", "light"] as const) {
  for (const width of [320, 1280]) {
    test(`rsync shared cards in ${scheme} at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ reducedMotion: "reduce" });
      await mockRsyncServer(page, {
        active: true,
        enabled: true,
        config: {
          module: "backup",
          path: "/srv/containers/a-very-long-directory-name-without-spaces/application-data/backups",
          username: "tnas",
          nas_address: "192.168.1.249",
          port: 873,
        },
      });
      await page.goto(
        scheme === "light" ? "/styling/light/rsync" : "/shares/rsync",
      );
      await expect(page.getByText("Running", { exact: true })).toBeVisible();
      await page.evaluate(() => document.fonts.ready);
      const module = page.getByRole("region", {
        name: "Rsync module",
        exact: true,
      });
      const ssh = page.getByRole("region", { name: "SSH backup connection" });
      await expect(module.locator(".frosted-card")).toBeVisible();
      await expect(ssh.locator(".frosted-card")).toBeVisible();
      const moduleBox = await module.boundingBox();
      const sshBox = await ssh.boundingBox();
      if (!moduleBox || !sshBox) throw new Error("Missing backup mode layout");
      if (width === 1280) {
        expect(Math.abs(moduleBox.y - sshBox.y)).toBeLessThan(1);
        expect(sshBox.x).toBeGreaterThan(moduleBox.x + moduleBox.width);
      } else {
        expect(sshBox.y).toBeGreaterThan(moduleBox.y + moduleBox.height);
      }
      const overflow = await page
        .locator(".rsync-page")
        .evaluate((element) => ({
          clientWidth: element.clientWidth,
          scrollWidth: element.scrollWidth,
        }));
      expect(overflow.scrollWidth).toBeLessThanOrEqual(overflow.clientWidth);
      await page.screenshot({
        path: test.info().outputPath(`rsync-${scheme}-${width}.png`),
        fullPage: true,
      });
      const save = ssh.getByRole("button", { name: "Save SSH module" });
      await ssh.getByLabel("SSH source folder", { exact: false }).focus();
      await page.keyboard.press("Tab");
      await page.keyboard.press("Tab");
      await expect(save).toBeFocused();
      await expect(save).toBeInViewport();
      await expect(save).toHaveCSS("outline-style", "solid");
      await page.keyboard.press("Enter");
      await expect(ssh).toContainText("rsync-fixture");
      if (width === 320) {
        await page.screenshot({
          path: test.info().outputPath(`rsync-${scheme}-320-ssh.png`),
          fullPage: true,
        });
      }
    });
  }
}
