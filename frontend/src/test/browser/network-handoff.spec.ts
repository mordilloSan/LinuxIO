import { expect, test, type WebSocketRoute } from "@playwright/test";

import type { NetworkBridgeHandoffStatus } from "../../api";

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

test("recovers a pending handoff after refresh without starting it twice", async ({
  page,
}) => {
  let operation: NetworkBridgeHandoffStatus | undefined;
  let starts = 0;
  const statusRequests: string[] = [];

  await page.routeWebSocket("**/ws", (socket) => {
    socket.onMessage((message) => {
      if (typeof message === "string" || message[4] !== 0x01) return;
      const id = message.readUInt32BE(0);
      const { route, request } = JSON.parse(message.subarray(14).toString());
      const reply = (data: unknown, missing = false) => {
        sendResult(
          socket,
          id,
          missing
            ? { status: "error", error: "network handoff not found", code: 404 }
            : { status: "ok", data },
        );
      };
      switch (route) {
        case "network.get_network_info":
          reply([]);
          break;
        case "monitoring.get_live":
          reply({ interfaces: {} });
          break;
        case "network.get_bridge_options":
          reply({
            candidates: [
              {
                name: "eth0",
                mac: "00:11:22:33:44:55",
                backend: "nmconnection",
                eligible: false,
                handoffEligible: true,
              },
            ],
          });
          break;
        case "network.start_bridge_handoff":
          starts++;
          operation = {
            operationId: request.operationId,
            name: request.name,
            member: request.member,
            backend: "nmconnection",
            state: "awaiting_confirmation",
            deadline: new Date(Date.now() + 90_000).toISOString(),
          };
          // Simulate a host mutation accepted before its reply reaches the browser.
          break;
        case "network.get_bridge_handoff":
          statusRequests.push(request.operationId);
          reply(operation, !operation);
          break;
        case "network.confirm_bridge_handoff":
          if (operation) {
            expect(request.operationId).toBe(operation.operationId);
            operation = { ...operation, state: "confirmed" };
            reply(operation);
          }
          break;
      }
    });
  });

  await page.goto("/network");
  await page
    .getByRole("button", { name: "Move host IP to bridge", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("checkbox").check();
  await dialog
    .getByRole("button", { name: "Move IP to bridge", exact: true })
    .click();
  await expect.poll(() => starts).toBe(1);
  const operationId = operation!.operationId;
  await expect(page).toHaveURL(new RegExp(operationId));

  await page.reload();
  await expect(
    dialog.getByRole("button", { name: "Confirm bridge", exact: true }),
  ).toBeEnabled();
  expect(statusRequests).toContain(operationId);
  expect(starts).toBe(1);
  await dialog
    .getByRole("button", { name: "Confirm bridge", exact: true })
    .click();
  await expect(
    dialog.getByText("Bridge confirmed", { exact: true }),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await expect(page).not.toHaveURL(new RegExp(operationId));
  await expect(dialog).toBeHidden();
  expect(starts).toBe(1);
});

test("can close a recovered URL whose operation does not exist", async ({
  page,
}) => {
  const operationId = "00000000-0000-4000-8000-000000000099";
  let statusRequests = 0;
  await page.routeWebSocket("**/ws", (socket) => {
    socket.onMessage((message) => {
      if (typeof message === "string" || message[4] !== 0x01) return;
      const id = message.readUInt32BE(0);
      const { route } = JSON.parse(message.subarray(14).toString());
      if (route === "network.get_bridge_handoff") {
        statusRequests++;
        sendResult(socket, id, {
          status: "error",
          error: "network handoff not found",
          code: 404,
        });
      } else {
        const data =
          route === "network.get_bridge_options"
            ? { candidates: [] }
            : route === "monitoring.get_live"
              ? { interfaces: {} }
              : [];
        sendResult(socket, id, { status: "ok", data });
      }
    });
  });

  await page.goto(`/network?handoffOperationId=${operationId}`);
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("button", { name: "Reset and retry", exact: true }),
  ).toBeEnabled();
  expect(statusRequests).toBeGreaterThanOrEqual(2);
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(page).not.toHaveURL(new RegExp(operationId));
});
