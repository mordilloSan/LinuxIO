import type * as acp from "@agentclientprotocol/sdk/unstable";

import type { Attachment } from "../attachments";

export type ToolBlock = {
  kind: "tool";
  id: string;
  toolCallId: string;
  title: string;
  toolKind: string;
  /** The agent's own name for the tool, when it sends one beside the kind. */
  toolName?: string;
  status: string;
  content: acp.ToolCallContent[];
  rawInput?: unknown;
  rawOutput?: unknown;
  /**
   * The call began a new step of the model: nothing the model said came just
   * before it, and no earlier call of the turn was still open.
   */
  opensStep?: boolean;
  /**
   * When the call started and ended (ms since epoch), if it ran in this page.
   * The agent says when a call ends but not when it starts to run, so the
   * start is the last time the call was heard of while still pending: the
   * model had finished writing its input by then.
   */
  startedAt?: number;
  endedAt?: number;
};

export type TranscriptBlock =
  | {
      kind: "user";
      id: string;
      text: string;
      attachments: Attachment[];
      /** When it was sent (ms since epoch), if it was sent from this page. */
      at?: number;
      /** The model the chat was set to when it was sent, if the agent named one. */
      model?: string;
    }
  | { kind: "agent"; id: string; text: string }
  | { kind: "thought"; id: string; text: string }
  | ToolBlock
  | { kind: "plan"; id: string; entries: acp.PlanEntry[] }
  | TurnBlock;

/** The end of a finished turn: how long it took and what it used. */
export type TurnBlock = {
  kind: "turn";
  id: string;
  durationMs: number;
  /** The agent's token count for the turn, when it reports one. */
  usage: acp.Usage | null;
  /** When the turn ended (ms since epoch). */
  at?: number;
};

/** A non-blank string field of an agent's untyped tool input, if it has one. */
export const stringField = (value: unknown, key: string) => {
  if (typeof value !== "object" || value === null) return null;
  const field = (value as Record<string, unknown>)[key];
  return typeof field === "string" && field.trim() ? field : null;
};

/** Claude Code's tool name, which its adapter sends as metadata. */
const toolNameOf = (update: { _meta?: Record<string, unknown> | null }) =>
  stringField(update._meta?.claudeCode, "toolName") ?? undefined;

let nextId = 0;
const newId = () => `b${++nextId}`;

const contentText = (content: acp.ContentBlock): string =>
  content.type === "text" ? content.text : `[${content.type}]`;

function appendChunk(
  blocks: readonly TranscriptBlock[],
  kind: "user" | "agent" | "thought",
  text: string,
): TranscriptBlock[] {
  const last = blocks[blocks.length - 1];
  if (last && last.kind === kind) {
    return [...blocks.slice(0, -1), { ...last, text: last.text + text }];
  }
  return [
    ...blocks,
    kind === "user"
      ? { kind, id: newId(), text, attachments: [] }
      : { kind, id: newId(), text },
  ];
}

export function appendUserMessage(
  blocks: readonly TranscriptBlock[],
  text: string,
  attachments: Attachment[],
  sent?: { at: number; model?: string },
): TranscriptBlock[] {
  return [...blocks, { kind: "user", id: newId(), text, attachments, ...sent }];
}

const ENDED = new Set(["completed", "failed"]);

/** Whether a tool call has ended, one way or the other. */
export const toolEnded = (status: string) => ENDED.has(status);

/** Whether a call added now would begin a new step of the model. */
function opensStep(blocks: readonly TranscriptBlock[]): boolean {
  const last = blocks[blocks.length - 1];
  if (last?.kind === "agent" || last?.kind === "thought") return false;
  for (let i = blocks.length - 1; i >= 0; i--) {
    const block = blocks[i];
    if (block.kind === "user" || block.kind === "turn") break;
    if (block.kind === "tool" && !toolEnded(block.status)) return false;
  }
  return true;
}

/** A tool block for a call first heard of now. */
function newToolBlock(
  blocks: readonly TranscriptBlock[],
  update: acp.ToolCall | acp.ToolCallUpdate,
  now: number | undefined,
): ToolBlock {
  const status = update.status ?? "pending";
  return {
    kind: "tool",
    id: newId(),
    toolCallId: update.toolCallId,
    title: update.title ?? update.toolCallId,
    toolKind: update.kind ?? "other",
    toolName: toolNameOf(update),
    status,
    content: update.content ?? [],
    rawInput: update.rawInput,
    rawOutput: update.rawOutput,
    opensStep: opensStep(blocks),
    // A call first heard of when it had ended was not seen running.
    startedAt: toolEnded(status) ? undefined : now,
    endedAt: toolEnded(status) ? now : undefined,
  };
}

export function appendTurnEnd(
  blocks: readonly TranscriptBlock[],
  durationMs: number,
  usage: acp.Usage | null,
  at?: number,
): TranscriptBlock[] {
  return [...blocks, { kind: "turn", id: newId(), durationMs, usage, at }];
}

/**
 * `now` is the time of an update that arrives while a turn runs; without it
 * (a chat's history being loaded) nothing is timed.
 */
export function applySessionUpdate(
  blocks: readonly TranscriptBlock[],
  update: acp.SessionUpdate,
  now?: number,
): TranscriptBlock[] {
  switch (update.sessionUpdate) {
    case "user_message_chunk":
      return appendChunk(blocks, "user", contentText(update.content));
    case "agent_message_chunk":
      return appendChunk(blocks, "agent", contentText(update.content));
    case "agent_thought_chunk":
      return appendChunk(blocks, "thought", contentText(update.content));
    case "tool_call":
      return [...blocks, newToolBlock(blocks, update, now)];
    case "tool_call_update": {
      const index = blocks.findIndex(
        (b) => b.kind === "tool" && b.toolCallId === update.toolCallId,
      );
      if (index < 0) return [...blocks, newToolBlock(blocks, update, now)];
      const current = blocks[index] as ToolBlock;
      const status = update.status ?? current.status;
      const merged: ToolBlock = {
        ...current,
        title: update.title ?? current.title,
        toolKind: update.kind ?? current.toolKind,
        toolName: toolNameOf(update) ?? current.toolName,
        status,
        content: update.content ?? current.content,
        rawInput: update.rawInput ?? current.rawInput,
        rawOutput: update.rawOutput ?? current.rawOutput,
        startedAt:
          current.status === "pending" && status === "pending"
            ? (now ?? current.startedAt)
            : current.startedAt,
        endedAt: current.endedAt ?? (toolEnded(status) ? now : undefined),
      };
      return [...blocks.slice(0, index), merged, ...blocks.slice(index + 1)];
    }
    case "plan": {
      const index = blocks.findIndex((b) => b.kind === "plan");
      const plan = {
        kind: "plan" as const,
        id: index < 0 ? newId() : blocks[index].id,
        entries: update.entries,
      };
      return index < 0
        ? [...blocks, plan]
        : [...blocks.slice(0, index), plan, ...blocks.slice(index + 1)];
    }
    default:
      return blocks as TranscriptBlock[];
  }
}
