/**
 * The subagents of a chat, read from the transcript the page already holds:
 * each subagent call, the calls its subagent made, what it said, and what it
 * says it used. Claude Code's adapter names the subagent call each call and
 * message of a subagent belongs to; an agent that does not shows its
 * subagents without them.
 */
import {
  stringField,
  type ToolBlock,
  toolEnded,
  type TranscriptBlock,
} from "../transcript/transcript";
import { labelOf, liveFrom, mcpOf, pathOf, span, spaced } from "./workMap";

export type RunStatus = "run" | "ok" | "err";

/** A call a subagent made, in words: `Read  btrfs.md  (lines 421-465)`. */
export interface AgentCall {
  id: string;
  /** The tool, e.g. `Read`, `Web Fetch`. */
  tool: string;
  /** What it worked on: a file's name, a command's description, an address. */
  detail: string;
  /** The detail is code: a path, a command, an address, a pattern. */
  code: boolean;
  /** A note beside it, e.g. `lines 421-465`. */
  note: string | null;
  /** The detail in full, e.g. the whole path or the command line. */
  full: string;
  status: RunStatus;
  tookMs: number | null;
}

export interface AgentRun {
  /** The subagent call's `toolCallId`. */
  id: string;
  label: string;
  /** The kind of subagent, e.g. `Explore`, when the call names one. */
  type: string | null;
  /** The model it was asked to run on, when the call names one. */
  model: string | null;
  /** It was sent to the background, so its call handed back at once. */
  background: boolean;
  /** The subagent that started it; null for the chat's agent. */
  startedBy: string | null;
  prompt: string | null;
  /** What it said, one entry per message, oldest first. */
  said: string[];
  /** What its call handed back; a subagent in the background hands back nothing of its work. */
  result: string | null;
  status: RunStatus;
  /** When it was seen to start (ms since epoch); null in a loaded chat. */
  startedAt: number | null;
  /** How long it took: its own account, else what was seen. */
  tookMs: number | null;
  /** Its own account of the tokens it used; only a live chat has it. */
  tokens: number | null;
  /** The calls it made, oldest first, without the subagents it started. */
  calls: AgentCall[];
  /** The subagents it started, oldest first. */
  agents: AgentRun[];
}

const SUBAGENT_TOOLS = new Set(["Agent", "Task"]);

/** Whether a call starts a subagent. */
export const isSubagentCall = (block: ToolBlock) =>
  block.toolName !== undefined && SUBAGENT_TOOLS.has(block.toolName);

const numberOf = (value: unknown, key: string) => {
  if (typeof value !== "object" || value === null) return null;
  const field = (value as Record<string, unknown>)[key];
  return typeof field === "number" && Number.isFinite(field) ? field : null;
};

// The note Claude Code hands back for a subagent it sent to the background.
// It is for the model, not for people, and it is all a loaded chat has to
// tell such a subagent by when the call did not ask for the background.
const LAUNCH_NOTE = /^Async agent launched\b/;

/**
 * Whether a subagent call sent its subagent to the background: the call
 * asked for it, its response says so, or it handed back the launch note.
 */
function sentToBackground(block: ToolBlock, result: string | null) {
  const input = block.rawInput as { run_in_background?: unknown } | undefined;
  return (
    input?.run_in_background === true ||
    block.launched === true ||
    (result !== null && LAUNCH_NOTE.test(result))
  );
}

/** The text a call handed back, once it has ended. */
function resultOf(block: ToolBlock): string | null {
  if (!toolEnded(block.status)) return null;
  const text = block.content
    .flatMap((item) =>
      item.type === "content" && item.content.type === "text"
        ? [item.content.text]
        : [],
    )
    .join("\n\n")
    .trim();
  return text === "" ? null : text;
}

// Claude Code's tool names as a person reads them; any other stays as it is.
const TOOL_NAMES: Record<string, string> = {
  AskUserQuestion: "Ask",
  NotebookEdit: "Edit notebook",
  SendMessage: "Send message",
  TaskStop: "Stop task",
  TodoWrite: "Todo list",
  ToolSearch: "Search tools",
  WebFetch: "Web Fetch",
  WebSearch: "Web Search",
};

// An agent that sends no tool name: the call is named by its ACP kind.
const KIND_NAMES: Record<string, string> = {
  delete: "Delete",
  edit: "Edit",
  execute: "Run",
  fetch: "Fetch",
  move: "Move",
  read: "Read",
  search: "Search",
  think: "Think",
};

const baseName = (path: string) =>
  path.replaceAll("\\", "/").split("/").filter(Boolean).pop() ?? path;

/** The lines a read asked for: `lines 421-465`, `from line 80`. */
function linesOf(input: unknown): string | null {
  const offset = numberOf(input, "offset");
  const limit = numberOf(input, "limit");
  if (offset === null && limit === null) return null;
  const first = offset ?? 1;
  return limit === null
    ? `from line ${first}`
    : `lines ${first}-${first + limit - 1}`;
}

/** A call in words: the tool, what it worked on, and a note beside it. */
export function describeCall(
  block: ToolBlock,
): Pick<AgentCall, "tool" | "detail" | "code" | "note" | "full"> {
  const input = block.rawInput;
  const name = block.toolName;
  const plain = (tool: string, detail: string, full = detail) => ({
    tool,
    detail,
    code: false,
    note: null,
    full,
  });
  const code = (
    tool: string,
    detail: string,
    note: string | null = null,
    full = detail,
  ) => ({ tool, detail, code: true, note, full });

  if (!name) {
    return plain(KIND_NAMES[block.toolKind] ?? "Tool", block.title);
  }
  const mcp = mcpOf(name);
  if (mcp) return plain(spaced(mcp.tool), mcp.server);
  const tool = TOOL_NAMES[name] ?? name;
  const described = stringField(input, "description");
  switch (name) {
    case "Read":
    case "Write":
    case "Edit":
    case "NotebookEdit": {
      const path = pathOf(input) ?? "";
      return code(
        tool,
        baseName(path),
        name === "Read" ? linesOf(input) : null,
        path,
      );
    }
    case "Bash":
    case "PowerShell":
    case "Monitor": {
      const command = stringField(input, "command") ?? "";
      return described ? plain(tool, described, command) : code(tool, command);
    }
    case "Grep":
    case "Glob": {
      const pattern = stringField(input, "pattern") ?? "";
      const path = stringField(input, "path");
      return path
        ? code(tool, pattern, `in ${baseName(path)}`, `${pattern} in ${path}`)
        : code(tool, pattern);
    }
    case "WebFetch":
      return code(tool, stringField(input, "url") ?? "");
    case "WebSearch":
    case "ToolSearch": {
      const query = stringField(input, "query");
      return query === null ? plain(tool, "") : code(tool, `"${query}"`);
    }
    case "Skill":
      return code(tool, stringField(input, "skill") ?? "");
    case "Agent":
    case "Task":
      return plain(tool, labelOf(block));
  }
  return plain(tool, described ?? (block.title === name ? "" : block.title));
}

/**
 * The chat's subagents, oldest first, each with the subagents it started.
 * `running` says a turn is live: only then is a call that has not ended
 * still running.
 */
export function agentsOf(
  blocks: readonly TranscriptBlock[],
  running: boolean,
): AgentRun[] {
  const live = liveFrom(blocks, running);
  const byCall = new Map<string, AgentRun>();
  // When anything the subagent did was last seen to end.
  const lastEnd = new Map<AgentRun, number>();
  const ended = (agent: AgentRun, at: number | undefined) => {
    if (at !== undefined)
      lastEnd.set(agent, Math.max(lastEnd.get(agent) ?? at, at));
  };
  const top: AgentRun[] = [];

  blocks.forEach((block, index) => {
    if (block.kind !== "tool") return;
    const inTurn = index >= live;
    const failed = block.status === "failed";
    const parent =
      block.parentId === undefined ? undefined : byCall.get(block.parentId);
    if (isSubagentCall(block)) {
      const input = block.rawInput;
      const result = resultOf(block);
      const background = sentToBackground(block, result);
      // A subagent in the background hands back at once. Claude Code keeps
      // the turn that started it open until it ends and says nothing else of
      // its end, so it runs while that turn does.
      const runs = background
        ? inTurn && !failed
        : inTurn && !toolEnded(block.status);
      const agent: AgentRun = {
        id: block.toolCallId,
        label: labelOf(block),
        type: stringField(input, "subagent_type"),
        model: stringField(input, "model"),
        background,
        startedBy: parent?.label ?? null,
        prompt: stringField(input, "prompt"),
        said: (block.said ?? [])
          .map((message) => message.text.trim())
          .filter((text) => text !== ""),
        // Its call's answer only says that it was sent off.
        result: background ? null : result,
        status: runs ? "run" : failed ? "err" : "ok",
        startedAt: block.startedAt ?? null,
        tookMs:
          block.totals?.durationMs ??
          (background ? null : span(block.startedAt, block.endedAt)),
        tokens: block.totals?.tokens ?? null,
        calls: [],
        agents: [],
      };
      ended(agent, block.endedAt);
      byCall.set(block.toolCallId, agent);
      (parent?.agents ?? top).push(agent);
      return;
    }
    if (!parent) return;
    ended(parent, block.endedAt);
    parent.calls.push({
      id: block.id,
      ...describeCall(block),
      status:
        !toolEnded(block.status) && inTurn ? "run" : failed ? "err" : "ok",
      tookMs: span(block.startedAt, block.endedAt),
    });
  });

  const settle = (agent: AgentRun) => {
    agent.agents.forEach((child) => {
      settle(child);
      ended(agent, lastEnd.get(child));
    });
    if (
      agent.calls.some((call) => call.status === "run") ||
      agent.agents.some((child) => child.status === "run")
    ) {
      agent.status = "run";
    }
    // Without its own account, a subagent in the background took until the
    // last thing it did ended.
    if (agent.tookMs === null && agent.background && agent.status !== "run") {
      agent.tookMs = span(agent.startedAt ?? undefined, lastEnd.get(agent));
    }
  };
  top.forEach(settle);
  return top;
}

/** Every subagent, the ones they started included. */
export const allAgents = (agents: readonly AgentRun[]): AgentRun[] =>
  agents.flatMap((agent) => [agent, ...allAgents(agent.agents)]);

/** How many subagents run now, the ones they started included. */
export const runningAgents = (agents: readonly AgentRun[]) =>
  allAgents(agents).filter((agent) => agent.status === "run").length;

/** 950, 73.1k, 1.2M. */
export function formatTokens(tokens: number): string {
  if (tokens < 1000) return String(Math.round(tokens));
  if (tokens < 1_000_000) return `${(tokens / 1000).toFixed(1)}k`;
  return `${(tokens / 1_000_000).toFixed(1)}M`;
}

/** 0 s, 45 s, 1 min 18 s: whole seconds, for a clock that ticks. */
export function formatElapsed(ms: number): string {
  const seconds = Math.max(0, Math.floor(ms / 1000));
  if (seconds < 60) return `${seconds} s`;
  return `${Math.floor(seconds / 60)} min ${seconds % 60} s`;
}
