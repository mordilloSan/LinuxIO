import { expect, type Page, test } from "@playwright/test";

import {
  paneAttachAnswer,
  paneCapabilities,
  panePanesAnswer,
  panePushedRoster,
  paneRenderAnswer,
} from "../assistantPaneFixture";

const DATA = 0x04;
const BRIDGE_DATA = 0x81;
const SESSION = "s1";

function dataFrame(streamId: number, text: string): Buffer {
  const payload = Buffer.from(text, "utf8");
  const frame = Buffer.alloc(14 + payload.length);
  frame.writeUInt32BE(streamId, 0);
  frame[4] = DATA;
  frame[5] = BRIDGE_DATA;
  frame.writeUInt32BE(streamId, 6);
  frame.writeUInt32BE(payload.length, 10);
  payload.copy(frame, 14);
  return frame;
}

interface Line {
  id?: number | string;
  method?: string;
  linuxio?: string;
  conn?: number;
  request?: { subtype?: string };
}

/**
 * A fake bridge and agent: ACP far enough to open a chat, and one Claude Code
 * process whose plugin pane is the drawing recorded from a real session.
 */
async function fakeAgentWithPane(page: Page) {
  const paneRequests: string[] = [];
  await page.routeWebSocket("**/ws", (socket) => {
    let streamId: number | undefined;
    let buffer = "";
    const send = (message: unknown) => {
      if (streamId !== undefined) {
        socket.send(dataFrame(streamId, `${JSON.stringify(message)}\n`));
      }
    };
    const pane = (message: unknown) =>
      send({ conn: 1, linuxio: "pane", message });

    socket.onMessage((message) => {
      if (typeof message === "string") return;
      if (message[4] === 0x01) {
        const { route } = JSON.parse(message.subarray(14).toString()) as {
          route?: string;
        };
        if (route === "assistant.open") {
          streamId = message.readUInt32BE(0);
          buffer = "";
          send({ cwd: "/home/alice", linuxio: "ready" });
        }
        return;
      }
      if (message.readUInt32BE(0) !== streamId || message[5] !== BRIDGE_DATA) {
        return;
      }
      buffer += message.subarray(14).toString();
      let nl = buffer.indexOf("\n");
      while (nl >= 0) {
        const text = buffer.slice(0, nl);
        buffer = buffer.slice(nl + 1);
        nl = buffer.indexOf("\n");
        if (!text) continue;
        const line = JSON.parse(text) as Line;
        if (line.linuxio === "pane") {
          const subtype = line.request?.subtype ?? "";
          paneRequests.push(subtype);
          const response =
            subtype === "ui_attach"
              ? paneAttachAnswer
              : subtype === "ui_panes"
                ? panePanesAnswer
                : subtype === "ui_render"
                  ? paneRenderAnswer
                  : null;
          pane(
            response
              ? { type: "pane_response", id: line.id, ok: true, response }
              : { type: "pane_response", id: line.id, ok: false, error: "no" },
          );
          continue;
        }
        switch (line.method) {
          case "initialize":
            send({
              jsonrpc: "2.0",
              id: line.id,
              result: {
                protocolVersion: 1,
                agentCapabilities: { loadSession: true },
                authMethods: [],
              },
            });
            break;
          case "session/list":
            send({ jsonrpc: "2.0", id: line.id, result: { sessions: [] } });
            break;
          case "session/new":
            send({
              jsonrpc: "2.0",
              id: line.id,
              result: { sessionId: SESSION },
            });
            // The order a real session produces: the roster push, then ready.
            send({ conn: 1, event: "open", linuxio: "pane" });
            pane({ ...panePushedRoster, session_id: SESSION });
            pane({ type: "relay_ready", capabilities: paneCapabilities });
            break;
          default:
            if (line.id !== undefined) {
              send({ jsonrpc: "2.0", id: line.id, result: {} });
            }
        }
      }
    });
  });
  return paneRequests;
}

const centre = (box: { x: number; width: number } | null) =>
  box ? box.x + box.width / 2 : Number.NaN;

test("floats a plugin pane beside a chat that stays centred", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1920, height: 1000 });
  const paneRequests = await fakeAgentWithPane(page);
  await page.goto("/assistant");
  const input = page.getByLabel("Message");
  await expect(input).toBeEnabled();

  const card = page.getByRole("complementary", { name: "Work map" });
  await expect(card).toBeVisible();
  expect(paneRequests).toEqual(["ui_attach", "ui_panes", "ui_render"]);

  // The plugin's picture is an image, never markup in the page.
  const picture = card.getByRole("img");
  await expect(picture).toBeVisible();
  expect(await picture.getAttribute("src")).toMatch(/^data:image\/svg\+xml/);
  expect(await card.locator(".assistant-pane-card__body svg").count()).toBe(0);
  await expect
    .poll(() => picture.evaluate((img: HTMLImageElement) => img.naturalWidth))
    .toBeGreaterThan(0);

  // The chat column is where it is without a pane: centred in its area.
  const area = await page.locator(".assistant__body").boundingBox();
  const composer = await page.locator(".assistant__composer").boundingBox();
  expect(Math.abs(centre(composer) - centre(area))).toBeLessThan(2);
  const cardBox = await card.boundingBox();
  expect(cardBox!.x).toBeGreaterThan(composer!.x + composer!.width);

  // Dragging the left edge to the left widens the card, and it stays that
  // wide after a reload.
  const edge = card.getByRole("separator", { name: "Resize Work map" });
  const edgeBox = await edge.boundingBox();
  const grabX = edgeBox!.x + edgeBox!.width / 2;
  const grabY = edgeBox!.y + edgeBox!.height / 2;
  await page.mouse.move(grabX, grabY);
  await page.mouse.down();
  await page.mouse.move(grabX - 120, grabY, { steps: 6 });
  await page.mouse.up();
  await expect
    .poll(async () => (await card.boundingBox())!.width)
    .toBeCloseTo(cardBox!.width + 120, 0);
  await page.reload();
  await expect(card).toBeVisible();
  await expect
    .poll(async () => (await card.boundingBox())!.width)
    .toBeCloseTo(cardBox!.width + 120, 0);

  // Minimized, only the header is left; hidden, a header button brings it back.
  await card.getByRole("button", { name: "Minimize Work map" }).click();
  await expect(picture).toBeHidden();
  await card.getByRole("button", { name: "Open Work map" }).click();
  await expect(picture).toBeVisible();
  await card.getByRole("button", { name: "Hide Work map" }).click();
  await expect(card).toBeHidden();
  await page.getByRole("button", { name: "Show Work map" }).click();
  await expect(card).toBeVisible();
});

test("starts the pane as a pill where the margin cannot hold it", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await fakeAgentWithPane(page);
  await page.goto("/assistant");
  await expect(page.getByLabel("Message")).toBeEnabled();

  const card = page.getByRole("complementary", { name: "Work map" });
  await expect(card).toBeVisible();
  await expect(card.getByRole("img")).toBeHidden();

  const area = await page.locator(".assistant__body").boundingBox();
  const composer = await page.locator(".assistant__composer").boundingBox();
  expect(Math.abs(centre(composer) - centre(area))).toBeLessThan(2);

  // Opened on request, it lies over the chat without moving it.
  await card.getByRole("button", { name: "Open Work map" }).click();
  await expect(card.getByRole("img")).toBeVisible();
  const after = await page.locator(".assistant__composer").boundingBox();
  expect(after!.x).toBe(composer!.x);
});
