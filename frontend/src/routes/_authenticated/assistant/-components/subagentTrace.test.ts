/**
 * A real chat with Claude Code's adapter (claude-agent-acp 0.102.0), recorded
 * on 2026-11-13 with `subagents` declared, replayed through the store: two
 * subagents started at once (one in the background, which starts one of its
 * own), a second message to the first, then one that is stopped; and the same
 * chat loaded again. See `@/test/assistantSubagentTrace.json`.
 */
import type * as acp from "@agentclientprotocol/sdk/unstable";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  type AssistantConnection,
  type AssistantHandlers,
  textBlock,
} from "@/api/acp";
import trace from "@/test/assistantSubagentTrace.json";

import { assistantStore } from "./assistant-store";
import type { TranscriptBlock } from "./transcript/transcript";
import { type AgentRun, agentsOf, allAgents } from "./workmap/agents";
import { summarize } from "./workmap/workMap";

type Entry = { t: number; mark?: string; sessionId?: string; update?: unknown };

// The recording's first message was sent at this time.
const BASE = 1_800_000_000_000;

function connection(overrides: Partial<AssistantConnection>) {
  let handlers!: AssistantHandlers;
  const conn = {
    agent: "claude",
    cwd: "/srv/work",
    init: { protocolVersion: 1, authMethods: [] },
    canListSessions: true,
    canLoadSession: true,
    promptCapabilities: { image: false, embeddedContext: false },
    listSessions: vi.fn(async () => []),
    newSession: vi.fn(async () => ({
      sessionId: "chat",
      modes: null,
      configOptions: [],
    })),
    cancel: vi.fn(async () => {}),
    close: vi.fn(),
    ...overrides,
  } as unknown as AssistantConnection;
  assistantStore._setConnector(async (_agent, h) => {
    handlers = h;
    return conn;
  });
  const deliver = (entry: Entry) => {
    vi.setSystemTime(BASE + entry.t);
    handlers.onUpdate({
      sessionId: entry.sessionId ?? "",
      update: entry.update as acp.SessionUpdate,
    });
    // One update a frame, so each is seen at the time it came.
    assistantStore._flush();
  };
  return { conn, deliver };
}

/** The chat as its rows read: who a message went to or came from. */
const chatRows = (blocks: readonly TranscriptBlock[]) =>
  blocks.flatMap((block) => {
    switch (block.kind) {
      case "message":
        return [
          block.outgoing
            ? `to ${block.recipientSessionId}`
            : `from ${block.senderSessionId}`,
        ];
      case "tool":
        return [`call ${block.toolName}`];
      case "user":
      case "agent":
        return [block.kind];
      default:
        return [];
    }
  });

/** A subagent as its card reads: its parts, with the tools of its calls. */
const card = (agent: AgentRun) =>
  agent.parts.map((part) =>
    part.kind === "calls"
      ? `calls ${part.calls.map((call) => `${call.tool}:${call.status}`).join(" ")}`
      : part.kind === "messages"
        ? `messages ${part.texts.length}`
        : `${part.kind} ${part.number ?? ""}`.trim(),
  );

const view = () => {
  const { sessionId, blocks, subagents, status } = assistantStore.getState();
  const agents = agentsOf({ sessionId, blocks }, subagents, false);
  const byLabel = Object.fromEntries(
    allAgents(agents).map((agent) => [agent.label, agent]),
  );
  return {
    blocks,
    subagents,
    agents,
    byLabel,
    summary: summarize(blocks, status === "running", subagents),
  };
};

// What every subagent's card shows, live and loaded alike.
const CARDS = {
  "Reader A": [
    "prompt 1",
    "calls Read:ok Bash:ok Write:ok",
    "report 1",
    "prompt 2",
    "messages 1",
    "report 2",
  ],
  "Reader B": ["prompt", "calls Read:ok Bash:ok", "report"],
  "Nested C": ["prompt", "calls Read:ok", "report"],
  // Its first `sleep 40` was refused, the second went to the background.
  Sleeper: ["prompt", "calls Bash:err Bash:ok Monitor:ok"],
};

beforeEach(() => {
  vi.useFakeTimers();
  assistantStore.disconnect();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("a recorded chat with subagents", () => {
  it("shows the live chat as it happened", async () => {
    let answer!: () => void;
    const { conn, deliver } = connection({
      prompt: vi.fn(
        () =>
          new Promise<acp.PromptResponse>((resolve) => {
            answer = () => resolve({ stopReason: "end_turn" });
          }),
      ),
    });
    await assistantStore.connect("claude");
    let sending: Promise<void> | undefined;
    for (const entry of trace.live as Entry[]) {
      vi.setSystemTime(BASE + entry.t);
      if (entry.mark === "prompt") {
        sending = assistantStore.send([textBlock("go")], {
          text: "go",
          attachments: [],
        });
      } else if (entry.mark === "answered") {
        answer();
        await sending;
      } else if (entry.mark?.startsWith("cancel:")) {
        // The Stop button of the Agents view; the adapter's answer follows.
        assistantStore.stopSubagent(entry.mark.slice("cancel:".length));
      } else {
        deliver(entry);
      }
    }
    expect(conn.cancel).toHaveBeenCalledExactlyOnceWith("sleeper");

    const { blocks, subagents, agents, byLabel, summary } = view();
    // No call for the launches, the message to Reader A or the hand-backs.
    expect(chatRows(blocks)).toEqual([
      "user",
      "call ToolSearch",
      "to reader-a",
      "to reader-b",
      "from reader-a",
      "to reader-a",
      "call ScheduleWakeup",
      "from reader-b",
      "agent",
      "from reader-a",
      "agent",
      "user",
      "to sleeper",
      "agent",
    ]);
    expect(
      subagents.map((child) => [
        child.sessionId,
        child.parentSessionId,
        child.state,
        child.runs.length,
      ]),
    ).toEqual([
      ["reader-a", "chat", "finished", 2],
      ["reader-b", "chat", "finished", 1],
      ["nested-c", "reader-b", "finished", 1],
      ["sleeper", "chat", "stopped", 1],
    ]);
    expect(agents.map((agent) => agent.label)).toEqual([
      "Reader A",
      "Reader B",
      "Sleeper",
    ]);
    expect(byLabel["Reader B"].agents.map((agent) => agent.label)).toEqual([
      "Nested C",
    ]);
    expect(byLabel["Nested C"].startedBy).toBe("Reader B");
    for (const [label, parts] of Object.entries(CARDS)) {
      expect(card(byLabel[label]), label).toEqual(parts);
    }
    expect(byLabel["Reader A"]).toMatchObject({
      type: "general-purpose",
      model: "Sonnet 5.5",
      state: "finished",
      // 7278 to 13591, then 15633 to 19877.
      ranMs: 6313 + 4244,
    });
    expect(byLabel.Sleeper).toMatchObject({ state: "stopped", canStop: false });

    // The map: four subagents, their calls at their stations, and each call
    // timed from when its input was written to its end.
    expect(summary.tally.agents).toMatchObject({ count: 4, open: 0 });
    const took = Object.fromEntries(
      [...blocks, ...subagents.flatMap((child) => child.blocks)].flatMap(
        (block) =>
          block.kind === "tool" &&
          block.startedAt !== undefined &&
          block.endedAt !== undefined
            ? [[block.title, block.endedAt - block.startedAt]]
            : [],
      ),
    );
    expect(took).toMatchObject({
      // Its input was still written at 7258; the hook's update at 7266 is
      // not the start.
      ToolSearch: 12,
      "wc -l b.txt": 1412,
      ScheduleWakeup: 601,
    });
    expect(summary.rows.some((row) => row.status === "run")).toBe(false);
  });

  it("shows the same chat when it is loaded again", async () => {
    const entries = trace.load as Entry[];
    const loaded = entries.findIndex((entry) => entry.mark === "loaded");
    const { deliver } = connection({
      listSessions: vi.fn(async () => [
        { sessionId: "chat", cwd: "/srv/work" },
      ]),
      loadSession: vi.fn(async (sessionId: string) => {
        // The transcript is replayed before the load is answered.
        entries.slice(0, loaded).forEach(deliver);
        return { sessionId, modes: null, configOptions: [] };
      }),
    });
    await assistantStore.connect("claude");
    // What each subagent is now comes after the answer.
    entries.slice(loaded + 1).forEach(deliver);

    const { blocks, subagents, byLabel, summary } = view();
    expect(assistantStore.getState().status).toBe("ready");
    // The adapter replays a subagent's whole transcript at its launch; each
    // report still stands after the message it answers.
    expect(chatRows(blocks)).toEqual([
      "user",
      "to reader-a",
      "from reader-a",
      "to reader-b",
      "from reader-b",
      "call ToolSearch",
      "to reader-a",
      "from reader-a",
      "call ScheduleWakeup",
      "agent",
      "user",
      "to sleeper",
      "agent",
    ]);
    expect(
      subagents.map((child) => [
        child.sessionId,
        child.parentSessionId,
        child.state,
        child.canCancel,
        child.runs.length,
      ]),
    ).toEqual([
      ["reader-a", "chat", "finished", false, 0],
      ["reader-b", "chat", "finished", false, 0],
      ["nested-c", "reader-b", "finished", false, 0],
      ["sleeper", "chat", "stopped", false, 0],
    ]);
    // The cards read as they did live. The replay leaves one of Reader B's
    // calls without its end; it is not running.
    for (const [label, parts] of Object.entries(CARDS)) {
      expect(card(byLabel[label]), label).toEqual(parts);
    }
    expect(byLabel["Reader A"]).toMatchObject({
      model: "Sonnet 5.5",
      ranMs: null,
    });
    expect(summary.tally.agents).toMatchObject({ count: 4, open: 0 });
    expect(summary.rows.some((row) => row.status === "run")).toBe(false);
  });
});
