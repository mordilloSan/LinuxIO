import { describe, expect, it } from "vitest";

import type { ToolBlock, TranscriptBlock } from "../transcript/transcript";
import {
  agentsOf,
  allAgents,
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
      toolUses: 2,
    });
    expect(agent.calls).toMatchObject([
      { station: "shell", label: "List disks", status: "ok", tookMs: 500 },
      { station: "files", label: "mdadm/mdadm.conf", status: "err" },
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
    expect(agent).toMatchObject({
      tokens: 73_100,
      toolUses: 9,
      tookMs: 78_000,
    });
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
    expect(agents[0].toolUses).toBe(1);
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
    // Sent to the background: its call ends at once and it works on.
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

  it("finds no subagents for an agent that names no tools", () => {
    expect(agentsOf([user, tool({ toolKind: "think" })], false)).toEqual([]);
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
