import type * as acp from "@agentclientprotocol/sdk";

export type ToolBlock = {
  kind: "tool";
  id: string;
  toolCallId: string;
  title: string;
  toolKind: string;
  status: string;
  content: acp.ToolCallContent[];
  rawInput?: unknown;
  rawOutput?: unknown;
};

export type TranscriptBlock =
  | { kind: "user"; id: string; text: string; attachments: string[] }
  | { kind: "agent"; id: string; text: string }
  | { kind: "thought"; id: string; text: string }
  | ToolBlock
  | { kind: "plan"; id: string; entries: acp.PlanEntry[] };

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
  attachments: string[],
): TranscriptBlock[] {
  return [...blocks, { kind: "user", id: newId(), text, attachments }];
}

export function applySessionUpdate(
  blocks: readonly TranscriptBlock[],
  update: acp.SessionUpdate,
): TranscriptBlock[] {
  switch (update.sessionUpdate) {
    case "user_message_chunk":
      return appendChunk(blocks, "user", contentText(update.content));
    case "agent_message_chunk":
      return appendChunk(blocks, "agent", contentText(update.content));
    case "agent_thought_chunk":
      return appendChunk(blocks, "thought", contentText(update.content));
    case "tool_call":
      return [
        ...blocks,
        {
          kind: "tool",
          id: newId(),
          toolCallId: update.toolCallId,
          title: update.title,
          toolKind: update.kind ?? "other",
          status: update.status ?? "pending",
          content: update.content ?? [],
          rawInput: update.rawInput,
          rawOutput: update.rawOutput,
        },
      ];
    case "tool_call_update": {
      const index = blocks.findIndex(
        (b) => b.kind === "tool" && b.toolCallId === update.toolCallId,
      );
      if (index < 0) {
        return [
          ...blocks,
          {
            kind: "tool",
            id: newId(),
            toolCallId: update.toolCallId,
            title: update.title ?? update.toolCallId,
            toolKind: update.kind ?? "other",
            status: update.status ?? "pending",
            content: update.content ?? [],
            rawInput: update.rawInput,
            rawOutput: update.rawOutput,
          },
        ];
      }
      const current = blocks[index] as ToolBlock;
      const merged: ToolBlock = {
        ...current,
        title: update.title ?? current.title,
        toolKind: update.kind ?? current.toolKind,
        status: update.status ?? current.status,
        content: update.content ?? current.content,
        rawInput: update.rawInput ?? current.rawInput,
        rawOutput: update.rawOutput ?? current.rawOutput,
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
