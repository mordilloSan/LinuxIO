import { expect, test } from "@playwright/test";

const DATA = 0x04;
const BRIDGE_DATA = 0x81;

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

type AcpMessage = {
  id?: number;
  method?: string;
  params?: {
    sessionId?: string;
    prompt?: Array<{ text: string }>;
    configId?: string;
    value?: string;
  };
  result?: { outcome?: { optionId?: string } };
};

const PERMISSION_REQUEST_ID = 500;

const modelOption = (currentValue: string) => ({
  id: "model",
  name: "Model",
  type: "select",
  category: "model",
  currentValue,
  options: [
    { value: "sonnet", name: "Sonnet" },
    { value: "opus", name: "Opus" },
  ],
});

test("chats with a fake ACP agent and approves a permission", async ({
  page,
}) => {
  const received: AcpMessage[] = [];
  await page.routeWebSocket("**/ws", (socket) => {
    // StrictMode may open more than once; always answer on the latest stream.
    let streamId: number | undefined;
    let buffer = "";
    let pendingPromptId: number | undefined;
    // Like the Claude adapter: a fork is not live until it is resumed.
    const registered = new Set<string>();
    const send = (message: unknown) => {
      if (streamId !== undefined) {
        socket.send(dataFrame(streamId, `${JSON.stringify(message)}\n`));
      }
    };
    const notify = (sessionId: string, update: unknown) =>
      send({
        jsonrpc: "2.0",
        method: "session/update",
        params: { sessionId, update },
      });

    socket.onMessage((message) => {
      if (typeof message === "string") return;
      if (message[4] === 0x01) {
        const { route } = JSON.parse(message.subarray(14).toString()) as {
          route?: string;
        };
        if (route === "assistant.open") {
          streamId = message.readUInt32BE(0);
          buffer = "";
          socket.send(
            dataFrame(streamId, '{"cwd":"/home/alice","linuxio":"ready"}\n'),
          );
        }
        return;
      }
      if (message.readUInt32BE(0) !== streamId || message[5] !== BRIDGE_DATA) {
        return;
      }
      buffer += message.subarray(14).toString();
      let nl = buffer.indexOf("\n");
      while (nl >= 0) {
        const line = buffer.slice(0, nl);
        buffer = buffer.slice(nl + 1);
        nl = buffer.indexOf("\n");
        if (!line) continue;
        const request = JSON.parse(line) as AcpMessage;
        received.push(request);
        switch (request.method) {
          case "initialize":
            send({
              jsonrpc: "2.0",
              id: request.id,
              result: {
                protocolVersion: 1,
                agentCapabilities: {
                  loadSession: true,
                  sessionCapabilities: {
                    list: {},
                    fork: {},
                    resume: {},
                    close: {},
                    delete: {},
                  },
                },
                authMethods: [],
              },
            });
            break;
          case "session/list":
            send({ jsonrpc: "2.0", id: request.id, result: { sessions: [] } });
            break;
          case "session/new":
            registered.add("s1");
            send({
              jsonrpc: "2.0",
              id: request.id,
              result: {
                sessionId: "s1",
                configOptions: [modelOption("sonnet")],
                modes: {
                  currentModeId: "default",
                  availableModes: [
                    { id: "default", name: "Default" },
                    { id: "plan", name: "Plan" },
                  ],
                },
              },
            });
            break;
          case "session/fork":
            // Returns a new id without registering it.
            send({
              jsonrpc: "2.0",
              id: request.id,
              result: { sessionId: "s2" },
            });
            break;
          case "session/resume":
          case "session/load":
            registered.add(request.params?.sessionId ?? "");
            send({ jsonrpc: "2.0", id: request.id, result: {} });
            break;
          case "session/close":
          case "session/delete":
            send({ jsonrpc: "2.0", id: request.id, result: {} });
            break;
          case "session/set_config_option":
            send({
              jsonrpc: "2.0",
              id: request.id,
              result: {
                configOptions: [modelOption(request.params?.value ?? "")],
              },
            });
            break;
          case "session/prompt": {
            const asked = request.params?.prompt?.[0]?.text ?? "";
            const promptSession = request.params?.sessionId ?? "";
            if (!registered.has(promptSession)) {
              send({
                jsonrpc: "2.0",
                id: request.id,
                error: { code: -32603, message: "Session not found" },
              });
              break;
            }
            if (promptSession !== "s1") {
              notify(promptSession, {
                sessionUpdate: "agent_message_chunk",
                content: { type: "text", text: `Side answer: ${asked}` },
              });
              send({
                jsonrpc: "2.0",
                id: request.id,
                result: { stopReason: "end_turn" },
              });
              break;
            }
            notify("s1", {
              sessionUpdate: "agent_message_chunk",
              content: {
                type: "text",
                text: `You asked: ${asked}\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n`,
              },
            });
            notify("s1", {
              sessionUpdate: "tool_call",
              toolCallId: "t1",
              title: "df -h",
              kind: "execute",
              status: "pending",
            });
            notify("s1", {
              sessionUpdate: "tool_call",
              toolCallId: "t2",
              title: "uptime",
              kind: "execute",
              status: "completed",
            });
            send({
              jsonrpc: "2.0",
              id: PERMISSION_REQUEST_ID,
              method: "session/request_permission",
              params: {
                sessionId: "s1",
                toolCall: { toolCallId: "t1", title: "df -h" },
                options: [
                  { optionId: "allow", name: "Allow once", kind: "allow_once" },
                  { optionId: "reject", name: "Reject", kind: "reject_once" },
                ],
              },
            });
            // The prompt result is sent after the permission answer arrives.
            pendingPromptId = request.id;
            break;
          }
          default:
            if (request.id === PERMISSION_REQUEST_ID) {
              const allowed = request.result?.outcome?.optionId === "allow";
              notify("s1", {
                sessionUpdate: "tool_call_update",
                toolCallId: "t1",
                status: allowed ? "completed" : "failed",
                content: [
                  {
                    type: "content",
                    content: {
                      type: "text",
                      text: "/dev/sda1  50G  20G  30G  40% /",
                    },
                  },
                ],
              });
              notify("s1", {
                sessionUpdate: "agent_message_chunk",
                content: {
                  type: "text",
                  text: allowed ? " Disk is 40% full." : " I did not run it.",
                },
              });
              send({
                jsonrpc: "2.0",
                id: pendingPromptId,
                result: { stopReason: "end_turn" },
              });
            }
        }
      }
    });
  });

  await page.goto("/assistant");
  const input = page.getByLabel("Message");
  await expect(input).toBeEnabled();

  await page.getByRole("button", { name: /^Model:/ }).click();
  await page.getByRole("menuitem", { name: /Opus/ }).click();
  await expect
    .poll(
      () =>
        received.find(
          (message) => message.method === "session/set_config_option",
        )?.params,
    )
    .toMatchObject({ configId: "model", value: "opus" });
  await expect(page.getByRole("button", { name: /^Model:/ })).toContainText(
    "Opus",
  );

  await input.fill("how full is the disk?");
  await page.keyboard.press("Enter");

  await expect(
    page.getByText("You asked: how full is the disk?"),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Stop" })).toBeVisible();
  await page.getByRole("button", { name: "Allow once" }).click();
  await expect(page.getByText(/Disk is 40% full/)).toBeVisible();
  await expect(page.getByRole("table")).toBeVisible();
  await expect(page.getByRole("cell", { name: "2" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Send" })).toBeVisible();
  await expect(input).toBeEnabled();

  await expect(page.getByText("2 tool calls")).toBeVisible();
  await page.getByText("2 tool calls").click();
  await expect(page.getByText("uptime")).toBeVisible();
  await expect(page.getByText("execute · completed")).toHaveCount(2);

  await page.locator(".assistant-tool__title", { hasText: "df -h" }).click();
  await expect(page.getByText("/dev/sda1  50G  20G  30G  40% /")).toBeVisible();

  // A side question forks the chat; the fork must be resumed before it answers.
  await page.getByLabel("Message", { exact: true }).fill("/btw is it healthy?");
  await page.keyboard.press("Enter");
  const side = page.getByRole("complementary", { name: "Side question" });
  await expect(side.getByText("Side answer: is it healthy?")).toBeVisible();
  await expect(page.getByText("Side answer: is it healthy?")).toHaveCount(1);
  await expect(page.locator(".assistant__main")).not.toContainText(
    "Side answer",
  );
  const methods = received.map((message) => message.method);
  expect(methods.indexOf("session/fork")).toBeGreaterThan(-1);
  expect(methods.indexOf("session/resume")).toBeGreaterThan(
    methods.indexOf("session/fork"),
  );
});
