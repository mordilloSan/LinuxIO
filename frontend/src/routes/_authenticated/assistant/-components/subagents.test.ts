import type * as acp from "@agentclientprotocol/sdk/unstable";
import { describe, expect, it } from "vitest";

import {
  applySubagentUpdate,
  disconnected,
  sentIn,
  type Subagent,
  workFor,
} from "./subagents";

const announce = (
  sessionId: string,
  over: Partial<acp.SubagentUpdate> = {},
): acp.SessionUpdate => ({
  sessionUpdate: "subagent_update",
  sessionId,
  title: `Agent ${sessionId}`,
  capabilities: { cancel: {} },
  state: { state: "running" },
  ...over,
});

const patch = (
  sessionId: string,
  over: Partial<acp.SubagentUpdate>,
): acp.SessionUpdate => ({
  sessionUpdate: "subagent_update",
  sessionId,
  ...over,
});

const message = (
  messageId: string,
  from: string,
  to: string,
): acp.SessionUpdate => ({
  sessionUpdate: "session_message",
  messageId,
  senderSessionId: from,
  recipientSessionId: to,
  content: [{ type: "text", text: messageId }],
});

const call = (toolCallId: string): acp.SessionUpdate => ({
  sessionUpdate: "tool_call",
  toolCallId,
  title: toolCallId,
});

describe("applySubagentUpdate", () => {
  it("adds a subagent when it is announced, under the session it came on", () => {
    const list = applySubagentUpdate(
      [],
      "chat",
      announce("a", { description: "Explore", cwd: "/srv" }),
      1000,
    );
    expect(list).toEqual([
      {
        sessionId: "a",
        parentSessionId: "chat",
        title: "Agent a",
        description: "Explore",
        cwd: "/srv",
        state: "running",
        error: null,
        canCancel: true,
        configOptions: [],
        blocks: [],
        runs: [{ startedAt: 1000, endedAt: null }],
      },
    ]);
    // One a subagent started is that subagent's.
    const nested = applySubagentUpdate(list, "a", announce("b"), 1100);
    expect(nested[1]).toMatchObject({ sessionId: "b", parentSessionId: "a" });
  });

  it("patches only what an update carries", () => {
    let list = applySubagentUpdate(
      [],
      "chat",
      announce("a", { description: "Explore", cwd: "/srv" }),
    );
    list = applySubagentUpdate(list, "chat", patch("a", { cwd: null }));
    expect(list[0]).toMatchObject({
      title: "Agent a",
      description: "Explore",
      cwd: null,
      state: "running",
      canCancel: true,
    });
    // `null` takes the capabilities away; an object replaces them all.
    list = applySubagentUpdate(
      list,
      "chat",
      patch("a", { capabilities: null }),
    );
    expect(list[0].canCancel).toBe(false);
    list = applySubagentUpdate(list, "chat", patch("a", { capabilities: {} }));
    expect(list[0].canCancel).toBe(false);
  });

  it.each([
    [{ state: "running" }, "running", null],
    [{ state: "requires_action" }, "waiting", null],
    [{ state: "idle", stopReason: "end_turn" }, "finished", null],
    [{ state: "idle", stopReason: "cancelled" }, "stopped", null],
    [
      {
        state: "idle",
        stopReason: "error",
        error: { code: 1, message: "Boom" },
      },
      "failed",
      "Boom",
    ],
    // An end the agent cannot explain, or explains in a way not known here.
    [{ state: "idle" }, "ended", null],
    [{ state: "idle", stopReason: "max_tokens" }, "ended", null],
    [{ state: "unknown" }, "disconnected", null],
    [null, "disconnected", null],
  ] as [acp.StateUpdate | null, string, string | null][])(
    "reads the state %o as %s",
    (state, expected, error) => {
      const [child] = applySubagentUpdate(
        applySubagentUpdate([], "chat", announce("a")),
        "chat",
        patch("a", { state }),
      );
      expect(child).toMatchObject({ state: expected, error });
    },
  );

  it("has no state for a subagent that was announced without one", () => {
    const [child] = applySubagentUpdate([], "chat", {
      sessionUpdate: "subagent_update",
      sessionId: "a",
    });
    expect(child).toMatchObject({
      state: "disconnected",
      title: null,
      canCancel: false,
    });
  });

  it("times each run that was seen, and none of a chat that was loaded", () => {
    let list = applySubagentUpdate([], "chat", announce("a"), 1000);
    // Waiting for the user is still the same run.
    list = applySubagentUpdate(
      list,
      "chat",
      patch("a", { state: { state: "requires_action" } }),
      1500,
    );
    list = applySubagentUpdate(
      list,
      "chat",
      patch("a", { state: { state: "idle", stopReason: "end_turn" } }),
      3000,
    );
    // Messaged again: it runs again, in the same session.
    list = applySubagentUpdate(
      list,
      "chat",
      patch("a", { state: { state: "running" } }),
      5000,
    );
    list = applySubagentUpdate(
      list,
      "chat",
      patch("a", { state: { state: "idle", stopReason: "end_turn" } }),
      5400,
    );
    expect(list[0].runs).toEqual([
      { startedAt: 1000, endedAt: 3000 },
      { startedAt: 5000, endedAt: 5400 },
    ]);

    let loaded = applySubagentUpdate([], "chat", announce("a"));
    loaded = applySubagentUpdate(
      loaded,
      "chat",
      patch("a", { state: { state: "idle", stopReason: "end_turn" } }),
    );
    expect(loaded[0]).toMatchObject({ state: "finished", runs: [] });
  });

  it("keeps what a subagent's session sends in its own blocks and options", () => {
    let list = applySubagentUpdate([], "chat", announce("a"), 1000);
    list = applySubagentUpdate(list, "a", message("p1", "chat", "a"), 1001);
    list = applySubagentUpdate(list, "a", call("c1"), 1002);
    list = applySubagentUpdate(
      list,
      "a",
      {
        sessionUpdate: "agent_message_chunk",
        content: { type: "text", text: "Looking." },
        _meta: { claudeCode: { parentToolUseId: "p1" } },
      },
      1003,
    );
    const model: acp.SessionConfigOption = {
      id: "model",
      name: "Model",
      category: "model",
      type: "select",
      currentValue: "opus",
      options: [{ value: "opus", name: "Opus 5.5" }],
    };
    list = applySubagentUpdate(list, "a", {
      sessionUpdate: "config_option_update",
      configOptions: [model],
    });
    expect(list[0].blocks).toMatchObject([
      { kind: "message", messageId: "p1", outgoing: false },
      { kind: "tool", toolCallId: "c1", startedAt: 1002 },
      { kind: "agent", text: "Looking." },
    ]);
    expect(list[0].configOptions).toEqual([model]);
  });

  it("leaves the list as it is for a session it does not know", () => {
    const list = applySubagentUpdate([], "chat", announce("a"));
    expect(applySubagentUpdate(list, "other", call("c1"))).toBe(list);
    // And for an update that changes nothing.
    expect(
      applySubagentUpdate(list, "a", {
        sessionUpdate: "available_commands_update",
        availableCommands: [],
      }),
    ).toBe(list);
  });
});

describe("workFor", () => {
  const child = (updates: acp.SessionUpdate[]): Subagent =>
    updates.reduce(
      (list, update) => applySubagentUpdate(list, "a", update),
      applySubagentUpdate([], "chat", announce("a")),
    )[0];

  it("gives the blocks from a message to the next one the subagent got", () => {
    const a = child([
      message("p1", "chat", "a"),
      call("c1"),
      message("r1", "a", "chat"),
      message("p2", "chat", "a"),
      call("c2"),
    ]);
    const sent = new Set(["p1", "p2"]);
    const ids = (messageId: string) =>
      workFor(a, messageId, sent).flatMap((b) =>
        b.kind === "tool" ? [b.toolCallId] : [],
      );
    expect(ids("p1")).toEqual(["c1"]);
    expect(ids("p2")).toEqual(["c2"]);
    expect(ids("p9")).toEqual([]);
    // Work for a message the sender does not show stays with the one before.
    expect(
      workFor(a, "p1", new Set(["p1"])).flatMap((b) =>
        b.kind === "tool" ? [b.toolCallId] : [],
      ),
    ).toEqual(["c1", "c2"]);
  });

  it("lists the ids of the messages a session sent", () => {
    const a = child([message("p1", "chat", "a"), message("r1", "a", "chat")]);
    expect([...sentIn(a.blocks)]).toEqual(["r1"]);
  });
});

describe("disconnected", () => {
  it("ends what ran and takes the stop away from all", () => {
    let list = applySubagentUpdate([], "chat", announce("a"), 1000);
    list = applySubagentUpdate(list, "chat", announce("b"), 1000);
    list = applySubagentUpdate(
      list,
      "chat",
      patch("b", { state: { state: "idle", stopReason: "end_turn" } }),
      2000,
    );
    const [a, b] = disconnected(list);
    expect(a).toMatchObject({ state: "disconnected", canCancel: false });
    expect(a.runs[0].endedAt).not.toBeNull();
    expect(b).toMatchObject({ state: "finished", canCancel: false });
  });
});
