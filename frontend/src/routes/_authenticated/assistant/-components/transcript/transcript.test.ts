import type * as acp from "@agentclientprotocol/sdk/unstable";
import { describe, expect, it } from "vitest";

import {
  appendUserMessage,
  applySessionUpdate,
  type TranscriptBlock,
} from "./transcript";

const text = (t: string): acp.ContentBlock => ({ type: "text", text: t });

describe("applySessionUpdate", () => {
  it("appends consecutive agent chunks to one block", () => {
    let blocks: TranscriptBlock[] = [];
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "agent_message_chunk",
      content: text("Hel"),
    });
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "agent_message_chunk",
      content: text("lo"),
    });
    expect(blocks).toHaveLength(1);
    expect(blocks[0]).toMatchObject({ kind: "agent", text: "Hello" });
  });

  it("starts a new agent block after a tool call", () => {
    let blocks: TranscriptBlock[] = [];
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "agent_message_chunk",
      content: text("a"),
    });
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "tool_call",
      toolCallId: "t1",
      title: "ls",
      kind: "execute",
      status: "pending",
    });
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "agent_message_chunk",
      content: text("b"),
    });
    expect(blocks.map((b) => b.kind)).toEqual(["agent", "tool", "agent"]);
  });

  it("merges tool_call_update into the matching tool block", () => {
    let blocks: TranscriptBlock[] = [];
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "tool_call",
      toolCallId: "t1",
      title: "ls",
      kind: "execute",
      status: "pending",
      rawInput: { cmd: "ls" },
    });
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "tool_call_update",
      toolCallId: "t1",
      status: "completed",
      content: [{ type: "content", content: text("file.txt") }],
    });
    expect(blocks).toHaveLength(1);
    expect(blocks[0]).toMatchObject({
      kind: "tool",
      status: "completed",
      rawInput: { cmd: "ls" },
      title: "ls",
    });
    expect(
      (blocks[0] as Extract<TranscriptBlock, { kind: "tool" }>).content,
    ).toHaveLength(1);
  });

  it("inserts a tool block when an update arrives for an unknown id", () => {
    const blocks = applySessionUpdate([], {
      sessionUpdate: "tool_call_update",
      toolCallId: "t9",
      status: "failed",
    });
    expect(blocks[0]).toMatchObject({
      kind: "tool",
      toolCallId: "t9",
      status: "failed",
      title: "t9",
    });
  });

  it("replaces the plan block in place", () => {
    let blocks: TranscriptBlock[] = [];
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "plan",
      entries: [{ content: "one", priority: "high", status: "pending" }],
    });
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "agent_message_chunk",
      content: text("x"),
    });
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "plan",
      entries: [{ content: "one", priority: "high", status: "completed" }],
    });
    expect(blocks.map((b) => b.kind)).toEqual(["plan", "agent"]);
    expect(blocks[0]).toMatchObject({ entries: [{ status: "completed" }] });
  });

  it("renders user and thought chunks as their own kinds and labels non-text content", () => {
    let blocks: TranscriptBlock[] = [];
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "user_message_chunk",
      content: text("why?"),
    });
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "agent_thought_chunk",
      content: text("hmm"),
    });
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "agent_message_chunk",
      content: { type: "image", data: "", mimeType: "image/png" },
    });
    expect(blocks.map((b) => b.kind)).toEqual(["user", "thought", "agent"]);
    expect(blocks[2]).toMatchObject({ text: "[image]" });
  });

  it("ignores update kinds it does not render", () => {
    const input: TranscriptBlock[] = [];
    const blocks = applySessionUpdate(input, {
      sessionUpdate: "available_commands_update",
      availableCommands: [],
    });
    expect(blocks).toEqual([]);
    expect(blocks).toBe(input);
  });

  it("appendUserMessage adds a user block with its attachments", () => {
    const pasted = { id: "p", kind: "text" as const, text: "a" };
    expect(appendUserMessage([], "hi", [pasted])[0]).toMatchObject({
      kind: "user",
      text: "hi",
      attachments: [pasted],
    });
  });

  it("ignores non-transcript updates and returns the same array", () => {
    const input = [] as never[];
    for (const update of [
      { sessionUpdate: "available_commands_update", availableCommands: [] },
      { sessionUpdate: "current_mode_update", currentModeId: "plan" },
      { sessionUpdate: "config_option_update", configOptions: [] },
      { sessionUpdate: "session_info_update", title: "x" },
      { sessionUpdate: "usage_update", used: 1, size: 2 },
    ] as acp.SessionUpdate[]) {
      expect(applySessionUpdate(input, update)).toBe(input);
    }
  });

  it("keeps the agent's own tool name beside the kind", () => {
    let blocks: TranscriptBlock[] = [];
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "tool_call",
      toolCallId: "c1",
      title: "Run tests",
      kind: "execute",
      _meta: { claudeCode: { toolName: "Bash" } },
    });
    expect(blocks[0]).toMatchObject({ toolName: "Bash" });
    // A later update without the name does not lose it.
    blocks = applySessionUpdate(blocks, {
      sessionUpdate: "tool_call_update",
      toolCallId: "c1",
      status: "completed",
    });
    expect(blocks[0]).toMatchObject({ toolName: "Bash", status: "completed" });
  });

  it("times a call only when it is told the time", () => {
    const call = (id: string): acp.SessionUpdate => ({
      sessionUpdate: "tool_call",
      toolCallId: id,
      title: id,
    });
    const done = (id: string): acp.SessionUpdate => ({
      sessionUpdate: "tool_call_update",
      toolCallId: id,
      status: "completed",
    });
    let blocks = applySessionUpdate([], call("c1"), 1000);
    blocks = applySessionUpdate(blocks, done("c1"), 1250);
    expect(blocks[0]).toMatchObject({ startedAt: 1000, endedAt: 1250 });
    // A later update does not move the end.
    blocks = applySessionUpdate(blocks, done("c1"), 9999);
    expect(blocks[0]).toMatchObject({ endedAt: 1250 });

    // The start is the last the call was heard of while pending: the model
    // was still writing its input before that.
    let refined = applySessionUpdate([], call("c3"), 1000);
    refined = applySessionUpdate(
      refined,
      { sessionUpdate: "tool_call_update", toolCallId: "c3", title: "c3 -a" },
      1400,
    );
    refined = applySessionUpdate(refined, done("c3"), 1500);
    expect(refined[0]).toMatchObject({ startedAt: 1400, endedAt: 1500 });

    const loaded = applySessionUpdate(
      applySessionUpdate([], call("c2")),
      done("c2"),
    );
    expect(loaded[0]).toMatchObject({
      startedAt: undefined,
      endedAt: undefined,
    });
  });

  it("marks the call that begins a new step of the model", () => {
    const call = (id: string): acp.SessionUpdate => ({
      sessionUpdate: "tool_call",
      toolCallId: id,
      title: id,
    });
    const done = (id: string): acp.SessionUpdate => ({
      sessionUpdate: "tool_call_update",
      toolCallId: id,
      status: "completed",
    });
    const updates: acp.SessionUpdate[] = [
      { sessionUpdate: "agent_thought_chunk", content: text("Hm") },
      // After the model spoke: part of the step its words began.
      call("c1"),
      // While c1 is open: beside it, in the same step.
      call("c2"),
      done("c1"),
      done("c2"),
      // Everything is back and the model said nothing: a new step.
      call("c3"),
    ];
    const blocks = updates.reduce<TranscriptBlock[]>(
      (acc, update) => applySessionUpdate(acc, update),
      appendUserMessage([], "go", []),
    );
    expect(
      blocks.flatMap((b) => (b.kind === "tool" ? [b.opensStep] : [])),
    ).toEqual([false, false, true]);
  });
});
