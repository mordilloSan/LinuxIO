/**
 * What an agent that reports its subagents as sessions sends, for tests: the
 * updates as Claude Code's adapter writes them, and a chat built from them.
 */
import type * as acp from "@agentclientprotocol/sdk/unstable";

import {
  applySubagentUpdate,
  type Subagent,
} from "@/routes/_authenticated/assistant/-components/subagents";
import {
  applySessionUpdate,
  type TranscriptBlock,
} from "@/routes/_authenticated/assistant/-components/transcript/transcript";

/** One update: the session it came on, the update, and when it was seen. */
export type Sent = [string, acp.SessionUpdate, number?];

const text = (t: string): acp.ContentBlock => ({ type: "text", text: t });

/** The announcement of a subagent, running and stoppable. */
export const announce = (
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

/** A change of a subagent's state. */
export const stateOf = (
  sessionId: string,
  state: acp.StateUpdate,
): acp.SessionUpdate => ({
  sessionUpdate: "subagent_update",
  sessionId,
  state,
});

export const FINISHED: acp.StateUpdate = {
  state: "idle",
  stopReason: "end_turn",
};

/** One copy of a message between two sessions. */
export const message = (
  messageId: string,
  from: string,
  to: string,
  body: string,
): acp.SessionUpdate => ({
  sessionUpdate: "session_message",
  messageId,
  senderSessionId: from,
  recipientSessionId: to,
  content: [text(body)],
});

/** Both copies of a message: the sender's, then the recipient's. */
export const pair = (
  messageId: string,
  from: string,
  to: string,
  body: string,
  at?: number,
): Sent[] => [
  [from, message(messageId, from, to, body), at],
  [to, message(messageId, from, to, body), at],
];

/** A tool call under Claude Code's name for the tool. */
export const call = (
  toolCallId: string,
  toolName: string,
  rawInput: object,
  status: acp.ToolCallStatus = "completed",
): acp.SessionUpdate => ({
  sessionUpdate: "tool_call",
  toolCallId,
  title: toolCallId,
  status,
  rawInput,
  _meta: { claudeCode: { toolName } },
});

/** The end of a call that was sent as `pending`. */
export const finish = (toolCallId: string): acp.SessionUpdate => ({
  sessionUpdate: "tool_call_update",
  toolCallId,
  status: "completed",
});

export const says = (body: string): acp.SessionUpdate => ({
  sessionUpdate: "agent_message_chunk",
  content: text(body),
});

/**
 * The chat whose session is `chat`, and its subagents, after everything in
 * `sent`, applied in order as the store does.
 */
export function chatOf(sent: Sent[], blocks: TranscriptBlock[] = []) {
  let subagents: Subagent[] = [];
  for (const [sessionId, update, at] of sent) {
    if (sessionId === "chat" && update.sessionUpdate !== "subagent_update") {
      blocks = applySessionUpdate(blocks, update, at, "chat");
    } else {
      subagents = applySubagentUpdate(subagents, sessionId, update, at);
    }
  }
  return { chat: { sessionId: "chat", blocks }, subagents };
}
