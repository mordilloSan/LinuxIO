import { describe, expect, it } from "vitest";

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

const subagent = (id: string, over: Partial<ToolBlock> = {}) =>
  tool({
    toolCallId: id,
    toolName: "Agent",
    toolKind: "think",
    rawInput: {
      description: `Agent ${id}`,
      prompt: `Do ${id}`,
      subagent_type: "Explore",
    },
    ...over,
  });

const user: TranscriptBlock = {
  kind: "user",
  id: "u1",
  text: "Check the disks",
  attachments: [],
};

describe("agentsOf", () => {
  it("puts each subagent's calls under it and leaves the agent's own out", () => {
    const [agent] = agentsOf(
      [
        user,
        tool({ toolName: "Read", rawInput: { file_path: "/etc/fstab" } }),
        subagent("a1", {
          content: [
            { type: "content", content: { type: "text", text: "All good." } },
          ],
          startedAt: 1000,
          endedAt: 79_000,
        }),
        tool({
          parentId: "a1",
          toolName: "Bash",
          rawInput: { command: "lsblk", description: "List disks" },
          startedAt: 2000,
          endedAt: 2500,
        }),
        tool({
          parentId: "a1",
          toolName: "Read",
          rawInput: { file_path: "/etc/mdadm/mdadm.conf" },
          status: "failed",
        }),
      ],
      false,
    );
    expect(agent).toMatchObject({
      id: "a1",
      label: "Agent a1",
      type: "Explore",
      prompt: "Do a1",
      result: "All good.",
      status: "ok",
      tookMs: 78_000,
      tokens: null,
      background: false,
      startedBy: null,
    });
    expect(agent.calls).toMatchObject([
      {
        tool: "Bash",
        detail: "List disks",
        code: false,
        full: "lsblk",
        status: "ok",
        tookMs: 500,
      },
      {
        tool: "Read",
        detail: "mdadm.conf",
        code: true,
        full: "/etc/mdadm/mdadm.conf",
        status: "err",
      },
    ]);
  });

  it("prefers what the subagent says it used to what was seen", () => {
    const [agent] = agentsOf(
      [
        subagent("a1", {
          startedAt: 1000,
          endedAt: 2000,
          totals: { tokens: 73_100, toolUses: 9, durationMs: 78_000 },
        }),
      ],
      false,
    );
    expect(agent).toMatchObject({ tokens: 73_100, tookMs: 78_000 });
  });

  it("nests the subagents a subagent started", () => {
    const agents = agentsOf(
      [
        subagent("a1"),
        subagent("a2", { parentId: "a1" }),
        tool({ parentId: "a2", toolName: "Grep" }),
        subagent("a3"),
      ],
      false,
    );
    expect(agents.map((agent) => agent.id)).toEqual(["a1", "a3"]);
    expect(agents[0].agents.map((agent) => agent.id)).toEqual(["a2"]);
    expect(agents[0].calls).toEqual([]);
    expect(agents[0].agents[0]).toMatchObject({ startedBy: "Agent a1" });
    expect(agents[0].agents[0].calls).toHaveLength(1);
    expect(allAgents(agents).map((agent) => agent.id)).toEqual([
      "a1",
      "a2",
      "a3",
    ]);
  });

  it("runs a subagent only while a turn does", () => {
    const blocks = [
      user,
      subagent("a1", { status: "in_progress", startedAt: 1000 }),
    ];
    expect(agentsOf(blocks, true)[0].status).toBe("run");
    expect(runningAgents(agentsOf(blocks, true))).toBe(1);
    // A chat that was loaded has nothing running.
    expect(agentsOf(blocks, false)[0].status).toBe("ok");
  });

  it("keeps a subagent that handed back running while its calls run", () => {
    const agents = agentsOf(
      [
        user,
        subagent("a1"),
        tool({ parentId: "a1", toolName: "Bash", status: "in_progress" }),
      ],
      true,
    );
    expect(agents[0].status).toBe("run");
  });

  it("runs a subagent in the background while the turn that started it does", () => {
    const launched = subagent("a1", {
      rawInput: { description: "Split replication", run_in_background: true },
      content: [
        {
          type: "content",
          content: { type: "text", text: "Async agent launched." },
        },
      ],
      startedAt: 1000,
      endedAt: 1200,
    });
    const blocks: TranscriptBlock[] = [
      user,
      launched,
      tool({
        parentId: "a1",
        toolName: "Grep",
        startedAt: 2000,
        endedAt: 9000,
      }),
    ];
    // Its call handed back at once, but the turn is held open for it.
    expect(agentsOf(blocks, true)[0]).toMatchObject({
      background: true,
      status: "run",
      tookMs: null,
      result: null,
    });
    // Once the turn ends it took until the last thing it did ended.
    expect(agentsOf(blocks, false)[0]).toMatchObject({
      status: "ok",
      tookMs: 8000,
    });
  });

  it("knows a subagent Claude Code sent to the background by itself", () => {
    const note = (text: string): Partial<ToolBlock> => ({
      content: [{ type: "content", content: { type: "text", text } }],
    });
    const [live, loaded, done] = agentsOf(
      [
        // Live: the response says so.
        subagent("a1", { launched: true }),
        // Loaded: only the note it handed back does.
        subagent("a2", note("Async agent launched successfully. agentId: x")),
        subagent("a3", note("Async work is done.")),
      ],
      false,
    );
    expect(live).toMatchObject({ background: true, result: null });
    expect(loaded).toMatchObject({ background: true, result: null });
    expect(done).toMatchObject({
      background: false,
      result: "Async work is done.",
    });
  });

  it("takes a subagent's report from its hand-back, not its launch note", () => {
    const [agent] = agentsOf(
      [
        subagent("a1", {
          launched: true,
          content: [
            {
              type: "content",
              content: {
                type: "text",
                text: "Async agent launched successfully.",
              },
            },
          ],
        }),
        tool({
          parentId: "a1",
          toolName: "Bash",
          rawInput: { command: "sleep 30", description: "Wait" },
        }),
        tool({
          parentId: "a1",
          toolName: "SubagentHandback",
          rawInput: { message: "Agent 3 done." },
        }),
      ],
      false,
    );
    expect(agent.result).toBe("Agent 3 done.");
    // The hand-back is the report, not one of its calls.
    expect(agent.calls.map((call) => call.tool)).toEqual(["Bash"]);
  });

  it("keeps what a subagent said, message by message", () => {
    const [agent] = agentsOf(
      [
        subagent("a1", {
          said: [
            { id: "m1", text: "Reading the docs." },
            { id: "m2", text: "  " },
            { id: "m3", text: "Waiting for the test run to finish.\n" },
          ],
        }),
      ],
      false,
    );
    expect(agent.said).toEqual([
      "Reading the docs.",
      "Waiting for the test run to finish.",
    ]);
  });

  it("finds no subagents for an agent that names no tools", () => {
    expect(agentsOf([user, tool({ toolKind: "think" })], false)).toEqual([]);
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
