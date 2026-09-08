import {
  expect,
  test,
  type Locator,
  type Page,
  type WebSocketRoute,
} from "@playwright/test";

interface HandoffOperation {
  operationId: string;
  name: string;
  member: string;
  backend: string;
  state: "awaiting_confirmation" | "confirmed" | "reverted";
  deadline: string;
}

function sendResult(socket: WebSocketRoute, id: number, result: unknown) {
  const body = Buffer.from(JSON.stringify(result));
  const frame = Buffer.alloc(14 + body.length);
  frame.writeUInt32BE(id, 0);
  frame[4] = 0x04;
  frame[5] = 0x85;
  frame.writeUInt32BE(id, 6);
  frame.writeUInt32BE(body.length, 10);
  body.copy(frame, 14);
  socket.send(frame);
}

async function installVMServer(
  page: Page,
  options: { dropStartReply?: boolean } = {},
) {
  const state = {
    bridgeCreated: false,
    confirms: 0,
    createRequests: [] as { member: string; name: string }[],
    handoff: undefined as HandoffOperation | undefined,
    reverts: 0,
    starts: 0,
  };
  const networks = () => [
    {
      active: true,
      hasPhysicalUplink: false,
      name: "default",
      type: "libvirt",
    },
    ...(state.bridgeCreated
      ? [
          {
            active: true,
            hasPhysicalUplink: true,
            name: "br-eth1",
            type: "bridge",
          },
        ]
      : []),
    ...(state.handoff?.state === "confirmed"
      ? [
          {
            active: true,
            hasPhysicalUplink: true,
            name: "br-eth0",
            type: "bridge",
          },
        ]
      : []),
  ];
  const candidates = {
    candidates: [
      {
        backend: "nmconnection",
        eligible: false,
        handoffEligible: true,
        handoffReasons: [],
        mac: "00:11:22:33:44:55",
        name: "eth0",
        reasons: ["Management interface"],
      },
      {
        backend: "nmconnection",
        eligible: true,
        handoffEligible: false,
        mac: "00:11:22:33:44:66",
        name: "eth1",
      },
    ],
  };

  await page.routeWebSocket("**/ws", (socket: WebSocketRoute) => {
    socket.onMessage((message) => {
      if (typeof message === "string" || message[4] !== 0x01) return;
      const id = message.readUInt32BE(0);
      const { request, route } = JSON.parse(message.subarray(14).toString());
      const reply = (data: unknown) =>
        sendResult(socket, id, { data, status: "ok" });
      switch (route) {
        case "filebrowser.exists_batch":
          reply({ existing: [{ isDir: true, path: "/isos" }] });
          break;
        case "filebrowser.directory_children":
          reply([]);
          break;
        case "network.create_bridge":
          state.bridgeCreated = true;
          state.createRequests.push({
            member: request.member,
            name: request.name,
          });
          reply({
            backend: "nmconnection",
            member: request.member,
            name: request.name,
          });
          break;
        case "network.get_bridge_handoff":
          if (state.handoff) {
            reply(state.handoff);
          } else {
            sendResult(socket, id, {
              code: 404,
              error: "network handoff not found",
              status: "error",
            });
          }
          break;
        case "network.get_bridge_options":
          reply(candidates);
          break;
        case "network.get_network_info":
          reply([
            {
              dns: [],
              gateway: "192.168.1.1",
              ipv4: ["192.168.1.20/24"],
              mac: "00:11:22:33:44:55",
              mtu: 1500,
              name: "eth0",
              operstate: "up",
              speed: "1G",
              state: 1,
              type: "ether",
            },
          ]);
          break;
        case "network.confirm_bridge_handoff":
          state.confirms++;
          if (state.handoff) {
            state.handoff = { ...state.handoff, state: "confirmed" };
          }
          reply(state.handoff);
          break;
        case "network.revert_bridge_handoff":
          state.reverts++;
          if (state.handoff) {
            state.handoff = { ...state.handoff, state: "reverted" };
          }
          reply(state.handoff);
          break;
        case "network.start_bridge_handoff":
          state.starts++;
          state.handoff = {
            backend: "nmconnection",
            deadline: new Date(Date.now() + 90_000).toISOString(),
            member: request.member,
            name: request.name,
            operationId: request.operationId,
            state: "awaiting_confirmation",
          };
          if (!options.dropStartReply) reply(state.handoff);
          break;
        case "virt.list":
          reply([]);
          break;
        case "virt.networks":
          reply(networks());
          break;
        case "virt.preflight":
          reply({
            defaultNetworkActive: true,
            defaultNetworkExists: true,
            defaultPoolActive: true,
            defaultPoolExists: true,
            errors: [],
            firmware: { biosAvailable: true, uefiAvailable: true },
            isoReadable: true,
            kvmPresent: true,
            libvirtReachable: true,
            managedPaths: {
              cloudImages: "/var/lib/libvirt/images/linuxio/cloud-images",
              isos: "/isos",
              root: "/var/lib/libvirt/images/linuxio",
            },
            qemuPresent: true,
            warnings: [],
          });
          break;
        case "virt.templates":
          reply({ path: "/templates", templates: [] });
          break;
        default:
          reply([]);
      }
    });
  });
  return state;
}

async function openCreateDialog(page: Page): Promise<Locator> {
  await page.goto("/vm");
  await page.getByRole("button", { name: "Create VM", exact: true }).click();
  return page.getByRole("dialog");
}

async function fillDraft(dialog: Locator, name = "bridge-vm") {
  await dialog.getByRole("textbox", { name: /^Name/ }).fill(name);
  await dialog
    .getByRole("textbox", { name: /^ISO path/ })
    .fill(`/isos/${name}.iso`);
}

test("creates a spare LAN bridge while retaining the VM draft and selecting it", async ({
  page,
}) => {
  const server = await installVMServer(page);
  const dialog = await openCreateDialog(page);
  await fillDraft(dialog, "spare-vm");
  await dialog
    .getByRole("button", { name: "Create LAN bridge", exact: true })
    .click();

  const bridgeDialog = page.getByRole("dialog");
  await expect(
    bridgeDialog.getByText("Create host bridge", { exact: true }),
  ).toBeVisible();
  await expect(
    bridgeDialog.getByRole("combobox", { name: "Spare wired NIC" }),
  ).toHaveText("eth1");
  await expect(
    bridgeDialog.getByLabel("Bridge name", { exact: true }),
  ).toHaveValue("br-eth1");
  await bridgeDialog
    .getByRole("button", { name: "Create bridge", exact: true })
    .click();

  const vmDialog = page.getByRole("dialog");
  await expect(vmDialog.getByText("Create VM", { exact: true })).toBeVisible();
  await expect(vmDialog.getByRole("textbox", { name: /^Name/ })).toHaveValue(
    "spare-vm",
  );
  await expect(
    vmDialog.getByRole("textbox", { name: /^ISO path/ }),
  ).toHaveValue("/isos/spare-vm.iso");
  await expect(vmDialog.getByRole("combobox", { name: "Network" })).toHaveText(
    "br-eth1",
  );
  expect(server.bridgeCreated).toBe(true);
  expect(server.createRequests).toEqual([{ member: "eth1", name: "br-eth1" }]);
});

test("recovers VM management handoff after refresh and selects the confirmed bridge", async ({
  page,
}) => {
  const server = await installVMServer(page, { dropStartReply: true });
  const dialog = await openCreateDialog(page);
  await dialog
    .getByRole("button", { name: "Create LAN bridge", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Use host network interface", exact: true })
    .click();

  const handoff = page.getByRole("dialog");
  await handoff.getByRole("checkbox").check();
  await handoff
    .getByRole("button", { name: "Move IP to bridge", exact: true })
    .click();
  await expect.poll(() => server.starts).toBe(1);
  const operationId = server.handoff!.operationId;
  await expect(page).toHaveURL(new RegExp(operationId));

  await page.reload();
  const recovered = page.getByRole("dialog");
  await expect(
    recovered.getByRole("button", { name: "Confirm bridge", exact: true }),
  ).toBeEnabled();
  expect(server.starts).toBe(1);
  await recovered
    .getByRole("button", { name: "Confirm bridge", exact: true })
    .click();
  await expect(
    recovered.getByText("Bridge confirmed", { exact: true }),
  ).toBeVisible();
  await recovered.getByRole("button", { name: "Close", exact: true }).click();
  await expect(page).not.toHaveURL(/handoffOperationId=/);
  expect(server.confirms).toBe(1);
  expect(server.starts).toBe(1);

  const vmDialog = page.getByRole("dialog");
  await expect(vmDialog.getByText("Create VM", { exact: true })).toBeVisible();
  await expect(vmDialog.getByRole("combobox", { name: "Network" })).toHaveText(
    "br-eth0",
  );
});

test("cancelling or reverting host handoff leaves the VM on NAT", async ({
  page,
}) => {
  const cancelled = await installVMServer(page);
  let dialog = await openCreateDialog(page);
  await fillDraft(dialog, "cancel-vm");
  await dialog
    .getByRole("button", { name: "Create LAN bridge", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Use host network interface", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Close", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await expect(dialog.getByText("Create VM", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("combobox", { name: "Network" })).toHaveText(
    "NAT (default)",
  );
  await expect(dialog.getByRole("textbox", { name: /^Name/ })).toHaveValue(
    "cancel-vm",
  );
  await expect(dialog.getByRole("textbox", { name: /^ISO path/ })).toHaveValue(
    "/isos/cancel-vm.iso",
  );
  expect(cancelled.starts).toBe(0);

  await dialog
    .getByRole("button", { name: "Create LAN bridge", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Use host network interface", exact: true })
    .click();
  const handoff = page.getByRole("dialog");
  await handoff.getByRole("checkbox").check();
  await handoff
    .getByRole("button", { name: "Move IP to bridge", exact: true })
    .click();
  await expect.poll(() => cancelled.starts).toBe(1);
  await expect(
    handoff.getByRole("button", { name: "Revert", exact: true }),
  ).toBeEnabled();
  await handoff.getByRole("button", { name: "Revert", exact: true }).click();
  await expect(
    handoff.getByText("Bridge handoff reverted", { exact: true }),
  ).toBeVisible();
  await handoff.getByRole("button", { name: "Close", exact: true }).click();
  await expect(page).not.toHaveURL(/handoffOperationId=/);
  expect(cancelled.reverts).toBe(1);
  dialog = page.getByRole("dialog");
  await expect(dialog.getByText("Create VM", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("combobox", { name: "Network" })).toHaveText(
    "NAT (default)",
  );
  await expect(dialog.getByRole("textbox", { name: /^Name/ })).toHaveValue(
    "cancel-vm",
  );
  await expect(dialog.getByRole("textbox", { name: /^ISO path/ })).toHaveValue(
    "/isos/cancel-vm.iso",
  );
});
