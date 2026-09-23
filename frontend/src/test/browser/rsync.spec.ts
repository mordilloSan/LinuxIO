import { expect, test } from "@playwright/test";

import type { RsyncSaveRequest, RsyncStatus } from "@/api";

test("configures a read-only backup module without a terminal", async ({
  page,
}) => {
  let status: RsyncStatus = {
    config: undefined,
    active: false,
    enabled: false,
  };
  const saves: RsyncSaveRequest[] = [];
  const sshPorts: number[] = [];
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
      const data =
        route === "shares.get_rsync_ssh"
          ? { available: true, port: request.port }
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
  await page.goto("/shares/rsync");
  const ssh = page.getByRole("region", { name: "SSH backup connection" });
  await expect(ssh.getByLabel("SSH port", { exact: false })).toHaveValue("22");
  await expect.poll(() => sshPorts).toContain(22);
  await ssh.getByLabel("SSH port", { exact: false }).fill("9222");
  await ssh.getByLabel("Linux username for SSH").fill("backup-user");
  await ssh.getByLabel("SSH source folder").fill("/home/backup-user/data");
  await ssh.getByRole("button", { name: "Check SSH port 9222" }).click();
  await expect.poll(() => sshPorts).toContain(9222);
  await expect(ssh).toContainText("9222");
  await expect(ssh).toContainText("backup-user");
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
