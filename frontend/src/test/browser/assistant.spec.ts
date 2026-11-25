import { readFileSync } from "node:fs";
import path from "node:path";

import { expect, type Locator, type Page, test } from "@playwright/test";

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

type FakeAgent = {
  send(message: unknown): void;
  notify(sessionId: string, update: unknown): void;
};

/**
 * A fake ACP agent behind the page's WebSocket: it opens the assistant's
 * stream, and hands `onRequest` each message the page sends on it.
 */
async function serveAgent(
  page: Page,
  onRequest: (request: AcpMessage, agent: FakeAgent) => void,
) {
  await page.routeWebSocket("**/ws", (socket) => {
    // StrictMode may open more than once; always answer on the latest stream.
    let streamId: number | undefined;
    let buffer = "";
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
        if (line) onRequest(JSON.parse(line) as AcpMessage, { send, notify });
      }
    });
  });
}

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

const effortOption = (currentValue: string) => ({
  id: "effort",
  name: "Effort",
  type: "select",
  category: "thought_level",
  currentValue,
  options: [
    { value: "low", name: "Low" },
    { value: "medium", name: "Medium" },
    { value: "high", name: "High" },
    { value: "max", name: "Max" },
  ],
});

test("chats with a fake ACP agent and approves a permission", async ({
  page,
}) => {
  const received: AcpMessage[] = [];
  let pendingPromptId: number | undefined;
  // Like the Claude adapter: a fork is not live until it is resumed.
  const registered = new Set<string>();
  await serveAgent(page, (request, { send, notify }) => {
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
      case "session/set_mode":
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
  });

  await page.goto("/assistant");
  const input = page.getByLabel("Message");
  await expect(input).toBeEnabled();

  // A new chat is switched to the Settings defaults before the prompt opens:
  // Opus and Plan. The fake agent offers no effort, so that one is skipped.
  await expect(page.getByRole("button", { name: "Mode: Plan" })).toBeVisible();
  const modelChanges = () =>
    received
      .filter((message) => message.method === "session/set_config_option")
      .map((message) => message.params);
  expect(modelChanges()).toMatchObject([{ configId: "model", value: "opus" }]);
  await expect(page.getByRole("button", { name: /^Model:/ })).toContainText(
    "Opus",
  );

  await page.getByRole("button", { name: /^Model:/ }).click();
  await page.getByRole("menuitem", { name: /Sonnet/ }).click();
  await expect
    .poll(() => modelChanges().at(-1))
    .toMatchObject({ configId: "model", value: "sonnet" });
  await expect(page.getByRole("button", { name: /^Model:/ })).toContainText(
    "Sonnet",
  );

  await input.fill("how full is the disk?");
  await page.keyboard.press("Enter");

  await expect(
    page.getByText("You asked: how full is the disk?"),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Stop" })).toBeVisible();

  // The activity map follows the turn: both calls went to the shell, and the
  // one still waiting keeps a ring spreading from its station.
  const activityTab = page.getByRole("button", {
    name: "Activity",
    exact: true,
  });
  await activityTab.click();
  await expect(
    page.getByRole("img", { name: /^Activity map\..*Shell 2/ }),
  ).toBeVisible();
  const beat = page.locator(".work-map__beat");
  await expect(beat).toHaveCount(1);
  expect(
    await beat.evaluate((ring) => ring.getAnimations().length),
  ).toBeGreaterThan(0);
  await activityTab.click();
  await expect(page.getByRole("img", { name: /^Activity map/ })).toHaveCount(0);

  await page.getByRole("button", { name: "Allow once" }).click();
  await expect(page.getByText(/Disk is 40% full/)).toBeVisible();
  await expect(page.getByRole("table")).toBeVisible();
  await expect(page.getByRole("cell", { name: "2" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Send" })).toBeVisible();
  await expect(input).toBeEnabled();

  // Both commands stand on one card; a line opens to what the command put out.
  const ran = page.getByRole("group", { name: "Ran on this server" });
  await expect(ran.getByText("uptime")).toBeVisible();
  await ran.getByRole("button", { name: /df -h/ }).click();
  await expect(page.getByText("/dev/sda1  50G  20G  30G  40% /")).toBeVisible();
  await ran.getByRole("button", { name: /df -h/ }).click();
  await expect(page.getByText("/dev/sda1  50G  20G  30G  40% /")).toBeHidden();

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

test("shows a subagent the agent reports as a session of its own", async ({
  page,
}) => {
  const received: AcpMessage[] = [];
  let promptId: number | undefined;
  const prompt = {
    sessionUpdate: "session_message",
    messageId: "launch1",
    senderSessionId: "s1",
    recipientSessionId: "child1",
    content: [{ type: "text", text: "Check how full the disks are" }],
  };
  await serveAgent(page, (request, { send, notify }) => {
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
              sessionCapabilities: { list: {} },
            },
            authMethods: [],
          },
        });
        break;
      case "session/list":
        send({ jsonrpc: "2.0", id: request.id, result: { sessions: [] } });
        break;
      case "session/new":
        send({ jsonrpc: "2.0", id: request.id, result: { sessionId: "s1" } });
        break;
      case "session/prompt":
        promptId = request.id;
        // The launch call is not sent: the subagent is announced on the
        // chat's session, with the prompt as a message in both sessions.
        notify("s1", {
          sessionUpdate: "subagent_update",
          sessionId: "child1",
          title: "Disk check",
          description: "Explore",
          capabilities: { cancel: {} },
          state: { state: "running" },
        });
        notify("s1", prompt);
        notify("child1", prompt);
        // Its own work comes on its own session id.
        notify("child1", {
          sessionUpdate: "tool_call",
          toolCallId: "t1",
          title: "df -h",
          kind: "execute",
          status: "pending",
          rawInput: { command: "df -h" },
          _meta: {
            claudeCode: { toolName: "Bash", parentToolUseId: "launch1" },
          },
        });
        notify("s1", {
          sessionUpdate: "subagent_update",
          sessionId: "child1",
          state: { state: "requires_action" },
        });
        send({
          jsonrpc: "2.0",
          id: PERMISSION_REQUEST_ID,
          method: "session/request_permission",
          params: {
            sessionId: "child1",
            toolCall: {
              toolCallId: "t1",
              title: "df -h",
              kind: "execute",
              rawInput: { command: "df -h" },
            },
            options: [
              { optionId: "allow", name: "Allow once", kind: "allow_once" },
              { optionId: "reject", name: "Reject", kind: "reject_once" },
            ],
          },
        });
        break;
      default:
        if (request.id !== PERMISSION_REQUEST_ID) break;
        notify("s1", {
          sessionUpdate: "subagent_update",
          sessionId: "child1",
          state: { state: "running" },
        });
        notify("child1", {
          sessionUpdate: "tool_call_update",
          toolCallId: "t1",
          status: "completed",
        });
        for (const sessionId of ["child1", "s1"]) {
          notify(sessionId, {
            sessionUpdate: "session_message",
            messageId: "handback1",
            senderSessionId: "child1",
            recipientSessionId: "s1",
            content: [{ type: "text", text: "The root disk is 40% full." }],
          });
        }
        notify("s1", {
          sessionUpdate: "subagent_update",
          sessionId: "child1",
          state: { state: "idle", stopReason: "end_turn" },
        });
        notify("s1", {
          sessionUpdate: "agent_message_chunk",
          content: { type: "text", text: "Nothing to worry about." },
        });
        send({
          jsonrpc: "2.0",
          id: promptId,
          result: { stopReason: "end_turn" },
        });
    }
  });

  await page.goto("/assistant");
  const input = page.getByLabel("Message");
  await expect(input).toBeEnabled();
  // The page says that it takes subagents as sessions.
  expect(
    received.find((message) => message.method === "initialize")?.params,
  ).toMatchObject({ clientCapabilities: { subagents: {} } });

  await input.fill("check the disks");
  await page.keyboard.press("Enter");

  // The chat shows the message to the subagent, and the subagent's request
  // under its name. Its call waits in the card, not under the message.
  const sentTo = page.locator(".assistant-message").first();
  await expect(sentTo).toContainText("Sent to");
  await expect(sentTo).toContainText("Disk check");
  await expect(page.getByText("Asked by Disk check.")).toBeVisible();
  await expect(page.getByRole("button", { name: "1 agent" })).toBeVisible();
  await expect(sentTo.locator(".assistant-tool")).toHaveCount(0);

  // The answer goes to the subagent's request, which lets the turn finish.
  await page.getByRole("button", { name: "Allow once" }).click();
  await expect(page.getByText("Nothing to worry about.")).toBeVisible();
  expect(
    received.find((message) => message.id === PERMISSION_REQUEST_ID)?.result,
  ).toMatchObject({ outcome: { optionId: "allow" } });
  await expect(sentTo.locator(".assistant-tool")).toHaveCount(1);
  await expect(sentTo).toContainText("df -h");
  await expect(page.getByText("The root disk is 40% full.")).toBeVisible();
  await expect(page.getByRole("button", { name: "0 agents" })).toBeVisible();

  // Its name opens the Agents card on it: the prompt, its call, its report.
  await sentTo.getByRole("button", { name: "Disk check" }).click();
  const agents = page.getByRole("complementary", { name: "Agents" });
  await expect(
    agents.getByRole("heading", { name: "Disk check" }),
  ).toBeVisible();
  await expect(agents.getByText("Finished · Explore")).toBeVisible();
  await expect(agents.getByText("Tool calls (1)")).toBeVisible();
  await expect(agents.getByText("Report", { exact: true })).toBeVisible();
  await expect(agents.getByText("The root disk is 40% full.")).toBeVisible();

  // The map counts the subagent and the call it made.
  await agents.getByRole("button", { name: "All agents" }).click();
  await agents.getByRole("button", { name: /Claude/ }).click();
  await expect(
    page.getByRole("img", { name: /^Activity map\..*Agents 1/ }),
  ).toBeVisible();
});

type TraceEntry = { mark?: string; sessionId?: string; update?: unknown };

test("replays a recorded chat with subagents, stops one and loads the chat again", async ({
  page,
}) => {
  // A real chat with Claude Code's adapter, recorded with `subagents`
  // declared: every update it sent, and the same chat being loaded.
  const trace = JSON.parse(
    readFileSync(
      path.resolve(process.cwd(), "src/test/assistantSubagentTrace.json"),
      "utf8",
    ),
  ) as { live: TraceEntry[]; load: TraceEntry[] };
  const received: AcpMessage[] = [];
  let at = 0;
  let promptId: number | undefined;
  let saved = false;
  // Sends the recording on from where it stands: to the end of the turn,
  // which answers the prompt, or to where the client stopped a subagent.
  const play = ({ send, notify }: FakeAgent) => {
    while (at < trace.live.length) {
      const entry = trace.live[at++];
      if (entry.mark === "answered") {
        send({
          jsonrpc: "2.0",
          id: promptId,
          result: { stopReason: "end_turn" },
        });
        return;
      }
      if (entry.mark?.startsWith("cancel:")) return;
      if (entry.sessionId) notify(entry.sessionId, entry.update);
    }
  };
  await serveAgent(page, (request, agent) => {
    received.push(request);
    const { send, notify } = agent;
    switch (request.method) {
      case "initialize":
        send({
          jsonrpc: "2.0",
          id: request.id,
          result: {
            protocolVersion: 1,
            agentCapabilities: {
              loadSession: true,
              sessionCapabilities: { list: {} },
            },
            authMethods: [],
          },
        });
        break;
      case "session/list":
        send({
          jsonrpc: "2.0",
          id: request.id,
          result: {
            sessions: saved ? [{ sessionId: "chat", cwd: "/home/alice" }] : [],
          },
        });
        break;
      case "session/new":
        send({ jsonrpc: "2.0", id: request.id, result: { sessionId: "chat" } });
        break;
      case "session/prompt":
        saved = true;
        promptId = request.id;
        play(agent);
        break;
      case "session/cancel":
        // The adapter stops that subagent and the turn goes on to its end.
        if (request.params?.sessionId === "sleeper") play(agent);
        break;
      case "session/load": {
        const loaded = trace.load.findIndex((entry) => entry.mark === "loaded");
        const replay = (entries: TraceEntry[]) =>
          entries.forEach((entry) => {
            if (entry.sessionId) notify(entry.sessionId, entry.update);
          });
        replay(trace.load.slice(0, loaded));
        send({ jsonrpc: "2.0", id: request.id, result: {} });
        replay(trace.load.slice(loaded + 1));
        break;
      }
    }
  });

  // Each message row as its head and the commands under it.
  const rows = () =>
    page
      .locator(".assistant-message")
      .evaluateAll((messages) =>
        messages.map((message) => [
          message.querySelector(".assistant-message__head")?.textContent,
          ...[...message.querySelectorAll(".assistant-tool")].map(
            (tool) =>
              tool.querySelector(
                ".assistant-tool__what, .assistant-tool__command",
              )?.textContent,
          ),
        ]),
      );
  const agentsCard = page.getByRole("complementary", { name: "Agents" });
  const parts = () =>
    agentsCard
      .locator(".agent-map--detail summary, .agent-map--detail h4")
      .allTextContents();

  await page.goto("/assistant");
  const input = page.getByLabel("Message");
  await expect(input).toBeEnabled();

  // The first turn: two subagents at once, one of them starting its own,
  // and a second message to the first.
  await input.fill("first");
  await page.keyboard.press("Enter");
  await expect(page.getByText(/When I asked, it said/)).toBeVisible();
  await expect(input).toBeEnabled();
  const live = [
    [
      "Sent toReader A",
      "Read a.txt",
      "Count lines in a.txt",
      "/srv/work/out-a.txt",
    ],
    // Reader B's own calls, then the subagent it started with its call.
    ["Sent toReader B", "Read b.txt", "Count lines in b.txt", "Read c.txt"],
    ["Sent toNested C", "Read c.txt"],
    ["Reader Areported"],
    // It answered the second message from what it knew: no calls.
    ["Sent toReader A"],
    ["Reader Breported"],
    ["Reader Areported"],
  ];
  expect(await rows()).toEqual(live);
  await expect(page.getByRole("button", { name: "0 agents" })).toBeVisible();

  // The second turn starts a subagent that is still at work.
  await input.fill("second");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("button", { name: "1 agent" })).toBeVisible();
  await page.getByRole("button", { name: "Sleeper" }).click();
  await expect(
    agentsCard.getByRole("heading", { name: "Sleeper" }),
  ).toBeVisible();
  await expect(
    agentsCard.getByText(/^Running · general-purpose/),
  ).toBeVisible();

  // Stop reaches the agent as a cancel of that subagent alone, and the
  // state the agent sends back is what the card then shows.
  await agentsCard.getByRole("button", { name: "Stop" }).click();
  await expect(
    agentsCard.getByText(/^Stopped · general-purpose/),
  ).toBeVisible();
  expect(
    received
      .filter((message) => message.method === "session/cancel")
      .map((message) => message.params?.sessionId),
  ).toEqual(["sleeper"]);
  await expect(agentsCard.getByRole("button", { name: "Stop" })).toHaveCount(0);
  await expect(
    page.getByText(/The Sleeper subagent didn't report/),
  ).toBeVisible();
  await expect(input).toBeEnabled();

  // A subagent that was messaged again, in the order it happened.
  await agentsCard.getByRole("button", { name: "All agents" }).click();
  await agentsCard.getByRole("button", { name: /Reader A/ }).click();
  const readerA = [
    "Prompt 1",
    "Tool calls (3)",
    "Report 1",
    "Prompt 2",
    "Messages (1)",
    "Report 2",
  ];
  expect(await parts()).toEqual(readerA);
  await expect(
    agentsCard.getByText("Finished · general-purpose · Sonnet 5.5"),
  ).toBeVisible();

  // The map: four subagents and the wakeup the chat scheduled at Agents,
  // and their calls at Files and Shell.
  await agentsCard.getByRole("button", { name: "All agents" }).click();
  await agentsCard.getByRole("button", { name: /Claude Code/ }).click();
  await expect(
    page.getByRole("img", { name: /^Activity map\..*Agents 5\b/ }),
  ).toBeVisible();

  // Loading the chat again brings all of it back, from the agent's replay.
  await page.reload();
  await expect(input).toBeEnabled();
  await expect(
    page.getByText(/The Sleeper subagent didn't report/),
  ).toBeVisible();
  expect(await rows()).toEqual([
    live[0],
    ["Reader Areported"],
    live[1],
    live[2],
    ["Reader Breported"],
    ["Sent toReader A"],
    ["Reader Areported"],
    [
      "Sent toSleeper",
      "Sleep for 40 seconds",
      "Sleep for 40 seconds in background",
      expect.any(String),
    ],
  ]);
  // Nothing of a loaded chat runs, whatever its replay left open.
  await expect(page.locator(".assistant-tool-state--active")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "0 agents" })).toBeVisible();
  await page.getByRole("button", { name: "Reader A" }).first().click();
  expect(await parts()).toEqual(readerA);
  await expect(agentsCard.getByText(/tokens not reported/)).toBeVisible();
});

type Rgb = [number, number, number];

/** The largest difference of one channel between two colours. */
const apart = (a: Rgb, b: Rgb) =>
  Math.max(...a.map((channel, index) => Math.abs(channel - b[index])));

/**
 * The element as drawn, read back one pixel at a time. Chromium computes no
 * style for a range input's thumb or track (getComputedStyle with
 * "::-webkit-slider-thumb" answers with the input's own box), so how they
 * look is read from a screenshot.
 */
async function drawn(page: Page, element: Locator) {
  const png = await element.screenshot({ animations: "disabled" });
  const { width, rgb } = await page.evaluate(async (base64) => {
    const image = new Image();
    image.src = `data:image/png;base64,${base64}`;
    await image.decode();
    const canvas = document.createElement("canvas");
    canvas.width = image.width;
    canvas.height = image.height;
    const context = canvas.getContext("2d")!;
    context.drawImage(image, 0, 0);
    const { data } = context.getImageData(0, 0, image.width, image.height);
    return {
      width: image.width,
      rgb: Array.from(data).filter((_, index) => index % 4 !== 3),
    };
  }, png.toString("base64"));
  return (x: number, y: number): Rgb => {
    const at = (y * width + x) * 3;
    return [rgb[at], rgb[at + 1], rgb[at + 2]];
  };
}

/**
 * The effort slider's thumb along its middle row: the longest run of the
 * accent colour, as its first and last pixel, counting the anti-aliased edge
 * pixels the thumb mostly covers. The dot of the level the thumb stands on
 * lies under it, and the other dots are too short to be the run.
 */
function thumbOf(
  pixel: (x: number, y: number) => Rgb,
  width: number,
  middle: number,
  accent: Rgb,
) {
  let best = { first: 0, last: -1 };
  let first = -1;
  for (let x = 0; x <= width; x++) {
    const inside = x < width && apart(pixel(x, middle), accent) <= 16;
    if (inside && first < 0) first = x;
    if (!inside && first >= 0) {
      if (x - 1 - first > best.last - best.first) best = { first, last: x - 1 };
      first = -1;
    }
  }
  return best;
}

test("draws the effort slider from the assistant's slider variables and moves it with the keys", async ({
  page,
}) => {
  const received: AcpMessage[] = [];
  const settings: Record<string, string> = {
    model: "sonnet",
    effort: "medium",
  };
  const configOptions = () => [
    modelOption(settings.model),
    effortOption(settings.effort),
  ];
  await serveAgent(page, (request, { send }) => {
    received.push(request);
    switch (request.method) {
      case "initialize":
        send({
          jsonrpc: "2.0",
          id: request.id,
          result: {
            protocolVersion: 1,
            agentCapabilities: { sessionCapabilities: { list: {} } },
            authMethods: [],
          },
        });
        break;
      case "session/list":
        send({ jsonrpc: "2.0", id: request.id, result: { sessions: [] } });
        break;
      case "session/new":
        send({
          jsonrpc: "2.0",
          id: request.id,
          result: { sessionId: "s1", configOptions: configOptions() },
        });
        break;
      case "session/set_config_option":
        settings[request.params?.configId ?? ""] = request.params?.value ?? "";
        send({
          jsonrpc: "2.0",
          id: request.id,
          result: { configOptions: configOptions() },
        });
        break;
    }
  });
  const effortChanges = () =>
    received
      .filter(
        (message) =>
          message.method === "session/set_config_option" &&
          message.params?.configId === "effort",
      )
      .map((message) => message.params?.value);

  await page.goto("/assistant");
  await expect(page.getByLabel("Message")).toBeEnabled();
  // The agent offers effort, so a new chat starts at the Settings default.
  const chip = page.getByRole("button", { name: /^Model:/ });
  await expect(chip).toHaveAccessibleName("Model: Opus High");
  expect(effortChanges()).toEqual(["high"]);

  // Opened from the keyboard, so focus rings are drawn.
  await chip.focus();
  await page.keyboard.press("Shift+Tab");
  await page.keyboard.press("Tab");
  await expect(chip).toBeFocused();
  await page.keyboard.press("Enter");
  const menu = page.getByRole("menu", { name: "Model" });
  await expect(menu.getByRole("menuitem", { name: /Sonnet/ })).toBeFocused();
  const slider = menu.getByRole("slider", { name: "Effort" });
  const label = menu.locator(".assistant__effort-label");
  await expect(slider).toHaveAttribute("aria-valuetext", "High");
  await expect(label).toHaveText("Effort (High)");

  // assistant.css sets the shared slider's variables on it: a 20px track
  // with no colour, so the dots behind it are the track, and a 12px thumb
  // with a 3px halo of the accent.
  const { accentHex, accent } = await page.evaluate(() => {
    const accentHex = getComputedStyle(document.documentElement)
      .getPropertyValue("--app-palette-primary-main")
      .trim();
    const probe = document.createElement("span");
    probe.style.color = accentHex;
    document.body.append(probe);
    const accent = getComputedStyle(probe)
      .color.match(/\d+/g)!
      .slice(0, 3)
      .map(Number) as Rgb;
    probe.remove();
    return { accentHex, accent };
  });
  expect(
    await slider.evaluate((input) =>
      Object.fromEntries(
        [
          "--app-slider-track-height",
          "--app-slider-track-color",
          "--app-slider-thumb-size",
          "--app-slider-thumb-shadow",
        ].map((name) => [
          name,
          getComputedStyle(input).getPropertyValue(name).trim(),
        ]),
      ),
    ),
  ).toEqual({
    "--app-slider-track-height": "20px",
    "--app-slider-track-color": "transparent",
    "--app-slider-thumb-size": "12px",
    "--app-slider-thumb-shadow": `0 0 0 3px color-mix(in srgb, ${accentHex}, transparent 75%)`,
  });

  // What is drawn follows them, not app-slider.css's own defaults (a 16px
  // thumb without a halo on a 4px track in the divider colour). The input
  // lies on a rounded surface whose side padding it leaves bare.
  const surface = menu.locator(".assistant__effort-slider");
  const outer = (await surface.boundingBox())!;
  const box = (await slider.boundingBox())!;
  const left = Math.round(box.x - outer.x);
  const right = left + Math.round(box.width);
  const width = Math.round(outer.width);
  const middle = Math.floor(outer.height / 2);
  const dots = await menu
    .locator(".assistant__effort-dot")
    .evaluateAll((nodes) =>
      nodes.map((node) => {
        const rect = node.getBoundingClientRect();
        return rect.left + rect.width / 2;
      }),
    );
  // Between the first two dots, where only the track could be drawn.
  const gap = Math.round((dots[0] + dots[1]) / 2 - outer.x);
  // Read before the slider has focus, whose ring is drawn in that padding.
  const bare = (await drawn(page, surface))(Math.floor(left / 2), middle);
  const expectDrawn = async (level: number) => {
    const pixel = await drawn(page, surface);
    // No track: between the dots the input draws nothing on the surface.
    expect(apart(pixel(gap, middle), bare)).toBeLessThanOrEqual(2);
    const thumb = thumbOf(pixel, width, middle, accent);
    // The thumb, 12px across where the travel puts this level.
    expect(thumb.last - thumb.first + 1).toBeGreaterThanOrEqual(11);
    expect(thumb.last - thumb.first + 1).toBeLessThanOrEqual(13);
    const centre = (thumb.first + thumb.last + 1) / 2;
    const travel = right - left - 12;
    expect(Math.abs(centre - (left + 6 + (level / 3) * travel))).toBeLessThan(
      1.5,
    );
    // The halo, 3px on each side that lies within the input (it ends at the
    // input's edge): a quarter of the accent over the surface. Past it, the
    // bare surface again.
    const quarter = bare.map((channel, index) =>
      Math.round(channel * 0.75 + accent[index] * 0.25),
    ) as Rgb;
    expect(apart(quarter, bare)).toBeGreaterThan(8);
    const sides = [
      { halo: thumb.first - 2, past: thumb.first - 5 },
      { halo: thumb.last + 2, past: thumb.last + 5 },
    ].filter(({ past }) => past >= left && past < right);
    expect(sides.length).toBeGreaterThan(0);
    for (const { halo, past } of sides) {
      expect(apart(pixel(halo, middle), quarter)).toBeLessThanOrEqual(3);
      expect(apart(pixel(past, middle), bare)).toBeLessThanOrEqual(2);
    }
  };
  await expectDrawn(2);

  // Hovering changes nothing: the halo is there at rest.
  await slider.hover();
  await expectDrawn(2);

  // The slider is a stop of the menu's arrow keys: from the first item,
  // ArrowUp wraps round to it. Focused, it keeps its halo and gains the
  // shared focus ring.
  await page.keyboard.press("ArrowUp");
  await expect(slider).toBeFocused();
  await expect(slider).toHaveCSS("outline-style", "solid");
  await expect(slider).toHaveCSS("outline-width", "2px");
  await expect(slider).toHaveCSS("outline-offset", "2px");
  await expect(slider).toHaveCSS("outline-color", `rgb(${accent.join(", ")})`);
  await expectDrawn(2);

  // Left, Right, Home and End move the level and send it once the key is up,
  // and stay on the slider.
  const press = async (key: string, level: string, sent: string[]) => {
    await page.keyboard.press(key);
    await expect(slider).toHaveAttribute("aria-valuetext", level);
    await expect(label).toHaveText(`Effort (${level})`);
    await expect.poll(effortChanges).toEqual(sent);
    await expect(slider).toBeFocused();
  };
  await press("ArrowRight", "Max", ["high", "max"]);
  await press("ArrowLeft", "High", ["high", "max", "high"]);
  await press("Home", "Low", ["high", "max", "high", "low"]);
  await expectDrawn(0);
  await press("End", "Max", ["high", "max", "high", "low", "max"]);
  await expectDrawn(3);

  // Down and Up leave it for the menu's items, without moving the level.
  await page.keyboard.press("ArrowDown");
  await expect(menu.getByRole("menuitem", { name: /Sonnet/ })).toBeFocused();
  await page.keyboard.press("ArrowUp");
  await expect(slider).toBeFocused();
  await expect(slider).toHaveAttribute("aria-valuetext", "Max");
  expect(effortChanges()).toEqual(["high", "max", "high", "low", "max"]);
  await expect(menu).toBeVisible();
  await expect(chip).toHaveAccessibleName("Model: Opus Max");
});

test("keeps a long answer with a code block inside the chat's own scroller", async ({
  page,
}) => {
  const answer = `${Array.from(
    { length: 60 },
    (_, index) => `Paragraph ${index} of a long answer.`,
  ).join("\n\n")}\n\n\`\`\`yaml\nname: value\n\`\`\`\n\nEnd.`;
  await serveAgent(page, (request, { send, notify }) => {
    switch (request.method) {
      case "initialize":
        send({
          jsonrpc: "2.0",
          id: request.id,
          result: {
            protocolVersion: 1,
            agentCapabilities: { sessionCapabilities: { list: {} } },
            authMethods: [],
          },
        });
        break;
      case "session/list":
        send({ jsonrpc: "2.0", id: request.id, result: { sessions: [] } });
        break;
      case "session/new":
        send({ jsonrpc: "2.0", id: request.id, result: { sessionId: "s1" } });
        break;
      case "session/prompt":
        notify("s1", {
          sessionUpdate: "agent_message_chunk",
          content: { type: "text", text: answer },
        });
        send({
          jsonrpc: "2.0",
          id: request.id,
          result: { stopReason: "end_turn" },
        });
        break;
    }
  });

  await page.goto("/assistant");
  const input = page.getByLabel("Message");
  await expect(input).toBeEnabled();
  await input.fill("hello");
  await input.press("Enter");
  await expect(page.getByText("End.")).toBeVisible();

  // The transcript scrolls; nothing in it reaches past the page, which would
  // let the page scroll under the chat and carry the prompt off the bottom.
  const transcript = page.getByRole("log").first();
  expect(
    await transcript.evaluate((el) => el.scrollHeight > el.clientHeight),
  ).toBe(true);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollHeight - window.innerHeight,
    ),
  ).toBe(0);
  await expect(input).toBeInViewport();
});

test("types a command in a terminal tab beside the chat, and reuses that tab", async ({
  page,
}) => {
  await serveAgent(page, (request, { send, notify }) => {
    switch (request.method) {
      case "initialize":
        send({
          jsonrpc: "2.0",
          id: request.id,
          result: {
            protocolVersion: 1,
            agentCapabilities: { sessionCapabilities: { list: {} } },
            authMethods: [],
          },
        });
        break;
      case "session/list":
        send({ jsonrpc: "2.0", id: request.id, result: { sessions: [] } });
        break;
      case "session/new":
        send({ jsonrpc: "2.0", id: request.id, result: { sessionId: "s1" } });
        break;
      case "session/prompt":
        notify("s1", {
          sessionUpdate: "agent_message_chunk",
          content: {
            type: "text",
            text: "First:\n\n```bash\nuptime\n```\n\nThen:\n\n```bash\ndf -h\n```",
          },
        });
        send({
          jsonrpc: "2.0",
          id: request.id,
          result: { stopReason: "end_turn" },
        });
        break;
    }
  });
  // The shell of the tab the chat opens: what reaches it was typed there.
  // Like bash, it has bracketed paste on exactly while it is at its prompt.
  let typed = "";
  let shellPrints: (text: string) => void = () => {};
  await page.context().routeWebSocket("**/ws", (socket) => {
    let shellId: number | undefined;
    socket.onMessage((message) => {
      if (typeof message === "string") return;
      if (message[4] === 0x01) {
        const { route } = JSON.parse(message.subarray(14).toString()) as {
          route?: string;
        };
        if (route !== "terminal.open") return;
        const id = message.readUInt32BE(0);
        shellId = id;
        shellPrints = (text) => socket.send(dataFrame(id, text));
        shellPrints("\u001b[?2004h$ ");
      } else if (
        message.readUInt32BE(0) === shellId &&
        message[5] === BRIDGE_DATA
      ) {
        typed += message.subarray(14).toString();
      }
    });
  });

  await page.goto("/assistant");
  const input = page.getByLabel("Message");
  await expect(input).toBeEnabled();
  await input.fill("hello");
  await input.press("Enter");
  const buttons = page.getByRole("button", { name: "Open in terminal" });
  await expect(buttons).toHaveCount(2);

  // The middle button opens the terminal in a tab of its own.
  const opening = page.waitForEvent("popup");
  await buttons.first().click({ button: "middle" });
  const terminalTab = await opening;
  await expect(terminalTab).toHaveURL(/\/terminal$/);
  await expect.poll(() => typed).toBe("uptime");

  // A plain click now types into that tab; the chat stays where it is.
  await buttons.last().click();
  await expect.poll(() => typed).toBe("uptimedf -h");
  await expect(page).toHaveURL(/\/assistant$/);
  expect(page.context().pages()).toHaveLength(2);
  await expect(buttons).toHaveCount(2);

  // While a program has the terminal, a command waits for the prompt.
  shellPrints("\u001b[?2004l\r\n");
  await buttons.first().click();
  await expect(
    terminalTab.getByText("A command from the Assistant is waiting"),
  ).toBeVisible();
  expect(typed).toBe("uptimedf -h");
  shellPrints("\u001b[?2004h$ ");
  await expect.poll(() => typed).toBe("uptimedf -huptime");
});
