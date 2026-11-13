import type * as acp from "@agentclientprotocol/sdk/unstable";
import { describe, expect, it } from "vitest";

import {
  announce,
  call,
  chatOf as chatFrom,
  FINISHED as done,
  pair,
  says,
  type Sent,
  stateOf as state,
} from "@/test/assistantSubagentFixture";

import type { ToolBlock, TranscriptBlock } from "../transcript/transcript";
import {
  agentsOf,
  allAgents,
  describeCall,
  formatElapsed,
  formatTokens,
  runningAgents,
} from "./agents";

let next = 0;
const tool = (over: Partial<ToolBlock> = {}): ToolBlock => ({
  kind: "tool",
  id: `t${++next}`,
  toolCallId: `call${next}`,
  title: "A call",
  toolKind: "other",
  status: "completed",
  content: [],
  ...over,
});

const subagentCall = (id: string, over: Partial<ToolBlock> = {}) =>
  tool({
    toolCallId: id,
    toolName: "Agent",
    toolKind: "think",
    rawInput: {
      description: `Agent ${id}`,
      prompt: `Do ${id}`,
      subagent_type: "Explore",
      model: "haiku",
    },
    ...over,
  });

const user: TranscriptBlock = {
  kind: "user",
  id: "u1",
  text: "Check the disks",
  attachments: [],
};

const text = (t: string): acp.ContentBlock => ({ type: "text", text: t });

/** A chat that began with the user's message. */
const chatOf = (sent: Sent[], blocks: TranscriptBlock[] = [user]) =>
  chatFrom(sent, blocks);

describe("agentsOf", () => {
  it("builds a subagent from its session: what it was told, did and reported", () => {
    const { chat, subagents } = chatOf([
      ["chat", announce("a", { description: "Explore", cwd: "/srv" }), 1000],
      ...pair("p1", "chat", "a", "Check the disks", 1000),
      [
        "a",
        {
          sessionUpdate: "config_option_update",
          configOptions: [
            {
              id: "model",
              name: "Model",
              category: "model",
              type: "select",
              currentValue: "claude-haiku",
              options: [{ value: "claude-haiku", name: "Haiku 5.5" }],
            },
            {
              id: "agent",
              name: "Agent",
              type: "select",
              currentValue: "disk-checker",
              options: [{ value: "disk-checker", name: "disk-checker" }],
            },
          ],
        },
      ],
      ["a", says("Looking at the disks.")],
      [
        "a",
        call("c1", "Bash", { command: "lsblk", description: "List disks" }),
      ],
      ["a", call("c2", "Read", { file_path: "/etc/mdadm.conf" }, "failed")],
      ...pair("h1", "a", "chat", "All good.", 79_000),
      ["chat", state("a", done), 79_000],
    ]);
    const [agent, ...rest] = agentsOf(chat, subagents, false);
    expect(rest).toEqual([]);
    expect(agent).toMatchObject({
      id: "a",
      label: "Agent a",
      // The agent option says more than the description does.
      type: "disk-checker",
      model: "Haiku 5.5",
      startedBy: null,
      state: "finished",
      status: "ok",
      error: null,
      cwd: "/srv",
      canStop: false,
      runningSince: null,
      ranMs: 78_000,
    });
    expect(agent.parts).toMatchObject([
      { kind: "prompt", number: null, text: "Check the disks" },
      { kind: "messages", texts: ["Looking at the disks."] },
      {
        kind: "calls",
        calls: [
          { tool: "Bash", detail: "List disks", full: "lsblk", status: "ok" },
          { tool: "Read", detail: "mdadm.conf", status: "err" },
        ],
      },
      { kind: "report", number: null, text: "All good." },
    ]);
  });

  it("falls back for the type to the description, then to the title", () => {
    const types = (over: Partial<acp.SubagentUpdate>) => {
      const { chat, subagents } = chatOf([["chat", announce("a", over)]]);
      return agentsOf(chat, subagents, false)[0].type;
    };
    expect(types({ description: "Explore" })).toBe("Explore");
    expect(types({})).toBe("Agent a");
  });

  it.each([
    [{ state: "running" }, "running", "run", true],
    [{ state: "requires_action" }, "waiting", "run", true],
    [done, "finished", "ok", false],
    [{ state: "idle", stopReason: "cancelled" }, "stopped", "ok", false],
    [
      {
        state: "idle",
        stopReason: "error",
        error: { code: 1, message: "Boom" },
      },
      "failed",
      "err",
      false,
    ],
    [{ state: "idle" }, "ended", "ok", false],
    [{ state: "unknown" }, "disconnected", "ok", false],
  ] as [acp.StateUpdate, string, string, boolean][])(
    "shows the state %o as %s",
    (value, expected, status, canStop) => {
      const { chat, subagents } = chatOf([
        ["chat", announce("a"), 1000],
        ["chat", state("a", value), 2000],
      ]);
      const agents = agentsOf(chat, subagents, true);
      expect(agents[0]).toMatchObject({ state: expected, status, canStop });
      expect(runningAgents(agents)).toBe(status === "run" ? 1 : 0);
    },
  );

  it("says why a subagent failed, and cannot stop one without the capability", () => {
    const { chat, subagents } = chatOf([
      ["chat", announce("a", { capabilities: null }), 1000],
      ["chat", announce("b"), 1000],
      [
        "chat",
        state("b", {
          state: "idle",
          stopReason: "error",
          error: { code: 1, message: "Out of credit" },
        }),
        2000,
      ],
    ]);
    const [a, b] = agentsOf(chat, subagents, true);
    expect(a).toMatchObject({ state: "running", canStop: false });
    expect(b).toMatchObject({ state: "failed", error: "Out of credit" });
  });

  it("shows a subagent that was messaged again in order, with the time of both runs", () => {
    const { chat, subagents } = chatOf([
      ["chat", announce("a"), 1000],
      ...pair("p1", "chat", "a", "Read the OS release", 1000),
      ["a", call("c1", "Read", { file_path: "/etc/os-release" })],
      ...pair("h1", "a", "chat", "It is Debian.", 3000),
      ["chat", state("a", done), 3000],
      // A SendMessage to it: it runs again in the same session.
      ["chat", state("a", { state: "running" }), 5000],
      ...pair("p2", "chat", "a", "Run uptime", 5000),
      ["a", call("c2", "Bash", { command: "uptime" })],
      ...pair("h2", "a", "chat", "Up 3 days.", 5400),
      ["chat", state("a", done), 5400],
    ]);
    const [agent] = agentsOf(chat, subagents, false);
    expect(agent.parts).toMatchObject([
      { kind: "prompt", number: 1, text: "Read the OS release" },
      { kind: "calls", calls: [{ tool: "Read" }] },
      { kind: "report", number: 1, text: "It is Debian." },
      { kind: "prompt", number: 2, text: "Run uptime" },
      { kind: "calls", calls: [{ tool: "Bash" }] },
      { kind: "report", number: 2, text: "Up 3 days." },
    ]);
    // The first run and the second, not the time between them.
    expect(agent).toMatchObject({ state: "finished", ranMs: 2400 });

    // While the second run goes, the status is that run's.
    const { chat: live, subagents: running } = chatOf([
      ["chat", announce("a"), 1000],
      ["chat", state("a", done), 3000],
      ["chat", state("a", { state: "running" }), 5000],
    ]);
    expect(agentsOf(live, running, true)[0]).toMatchObject({
      state: "running",
      ranMs: 2000,
      runningSince: 5000,
    });
  });

  it("takes the parent's reply for the report of a subagent that handed nothing back", () => {
    const { chat, subagents } = chatOf([
      ["chat", announce("a")],
      ...pair("p1", "chat", "a", "Check the disks"),
      ["a", call("c1", "Bash", { command: "lsblk" })],
      ["a", says("Looking.")],
      ["a", call("c2", "Bash", { command: "df" })],
      // Its answer is its own last text; only the parent gets the reply.
      ["a", says("All good.")],
      [
        "chat",
        {
          sessionUpdate: "session_message",
          messageId: "p1:reply",
          senderSessionId: "a",
          recipientSessionId: "chat",
          content: [text("All good.")],
        },
      ],
      ["chat", state("a", done)],
    ]);
    const [agent] = agentsOf(chat, subagents, false);
    expect(agent.parts.map((part) => part.kind)).toEqual([
      "prompt",
      "calls",
      "messages",
      "calls",
      "report",
    ]);
    expect(agent.parts.at(-1)).toMatchObject({ text: "All good." });
    // A chat that was loaded saw none of it happen.
    expect(agent).toMatchObject({ ranMs: null, runningSince: null });
  });

  it("nests the subagents a subagent started", () => {
    const { chat, subagents } = chatOf([
      ["chat", announce("a")],
      ...pair("p1", "chat", "a", "Do a"),
      ["a", announce("b")],
      ...pair("p2", "a", "b", "Do b"),
      ["b", call("c1", "Grep", { pattern: "x" })],
      ["chat", announce("c")],
      ...pair("p3", "chat", "c", "Do c"),
    ]);
    const agents = agentsOf(chat, subagents, true);
    expect(agents.map((agent) => agent.id)).toEqual(["a", "c"]);
    expect(agents[0].agents).toMatchObject([
      { id: "b", startedBy: "Agent a", parts: [{}, { kind: "calls" }] },
    ]);
    // What it told the one it started is that one's prompt, not its own.
    expect(agents[0].parts.map((part) => part.kind)).toEqual(["prompt"]);
    expect(allAgents(agents).map((agent) => agent.id)).toEqual(["a", "b", "c"]);
  });

  it("shows a subagent call that reaches the chat from the call alone", () => {
    const blocks: TranscriptBlock[] = [
      user,
      tool({ toolName: "Read", rawInput: { file_path: "/etc/fstab" } }),
      subagentCall("a1", {
        content: [
          { type: "content", content: { type: "text", text: "All good." } },
        ],
        startedAt: 1000,
        endedAt: 79_000,
      }),
      subagentCall("a2", { status: "failed" }),
      subagentCall("a3", { status: "in_progress", startedAt: 2000 }),
    ];
    const chat = { sessionId: "chat", blocks };
    const [done1, failed, open] = agentsOf(chat, [], true);
    expect(done1).toMatchObject({
      id: "a1",
      label: "Agent a1",
      type: "Explore",
      model: "haiku",
      state: "finished",
      status: "ok",
      canStop: false,
      ranMs: 78_000,
      parts: [
        { kind: "prompt", text: "Do a1" },
        { kind: "report", text: "All good." },
      ],
      agents: [],
    });
    expect(failed).toMatchObject({ state: "failed", status: "err" });
    expect(open).toMatchObject({ state: "running", runningSince: 2000 });
    // A chat that was loaded has nothing running.
    expect(agentsOf(chat, [], false)[2]).toMatchObject({
      state: "ended",
      status: "ok",
    });
  });

  it("draws one subagent for a launch a permission request showed", () => {
    // The call stays a tool call of the chat, and its subagent is announced
    // too: the prompt has the call's id.
    const { chat, subagents } = chatOf(
      [["chat", announce("a")], ...pair("a1", "chat", "a", "Do a1")],
      [user, subagentCall("a1")],
    );
    const agents = agentsOf(chat, subagents, true);
    expect(agents.map((agent) => agent.id)).toEqual(["a"]);
  });

  it("finds no subagents for an agent that names no tools", () => {
    const chat = {
      sessionId: "chat",
      blocks: [user, tool({ toolKind: "think" })],
    };
    expect(agentsOf(chat, [], false)).toEqual([]);
  });
});

describe("describeCall", () => {
  it.each([
    [
      {
        toolName: "Read",
        rawInput: { file_path: "/d/btrfs.md", offset: 421, limit: 45 },
      },
      {
        tool: "Read",
        detail: "btrfs.md",
        code: true,
        note: "lines 421-465",
        full: "/d/btrfs.md",
      },
    ],
    [
      { toolName: "Read", rawInput: { file_path: "/d/jobs.md", offset: 80 } },
      { tool: "Read", detail: "jobs.md", note: "from line 80" },
    ],
    [
      { toolName: "Bash", rawInput: { command: "ls -la /srv" } },
      { tool: "Bash", detail: "ls -la /srv", code: true, note: null },
    ],
    [
      {
        toolName: "Grep",
        rawInput: { pattern: "RunReplication", path: "/src/backend" },
      },
      {
        tool: "Grep",
        detail: "RunReplication",
        note: "in backend",
        full: "RunReplication in /src/backend",
      },
    ],
    [
      { toolName: "WebFetch", rawInput: { url: "https://systemd.io/x" } },
      { tool: "Web Fetch", detail: "https://systemd.io/x", code: true },
    ],
    [
      { toolName: "ToolSearch", rawInput: { query: "select:WebFetch" } },
      { tool: "Search tools", detail: '"select:WebFetch"', code: true },
    ],
    [
      { toolName: "mcp__claude-in-chrome__navigate_page", rawInput: {} },
      { tool: "navigate page", detail: "claude-in-chrome", code: false },
    ],
    [
      { toolKind: "execute", title: "make test" },
      { tool: "Run", detail: "make test", code: false },
    ],
  ] as [Partial<ToolBlock>, object][])("describes %o", (over, text) => {
    expect(describeCall(tool(over))).toMatchObject(text);
  });
});

describe("formatting", () => {
  it.each([
    [950, "950"],
    [73_100, "73.1k"],
    [275_800, "275.8k"],
    [1_200_000, "1.2M"],
  ])("writes %d tokens as %s", (tokens, text) => {
    expect(formatTokens(tokens)).toBe(text);
  });

  it.each([
    [-5, "0 s"],
    [45_900, "45 s"],
    [78_000, "1 min 18 s"],
  ])("writes %d ms on a clock as %s", (ms, text) => {
    expect(formatElapsed(ms)).toBe(text);
  });
});
