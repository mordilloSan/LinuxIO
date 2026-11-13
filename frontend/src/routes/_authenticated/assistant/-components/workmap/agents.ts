/**
 * The subagents of a chat, for the Agents view. An agent that reports its
 * subagents as sessions (subagents.ts) gives each one's own calls, messages,
 * prompts and reports; a subagent call that reaches the chat as a plain tool
 * call is shown from the call alone.
 */
import { currentName } from "../composer/selectOption";
import {
  isActive,
  sentIn,
  type Subagent,
  type SubagentState,
} from "../subagents";
import {
  type MessageBlock,
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

/**
 * One stretch of what a subagent did, in the order it happened. A subagent
 * that was messaged again has a second prompt, more calls and a second
 * report, so prompts and reports are numbered when there are several.
 */
export type AgentPart =
  | { kind: "prompt"; id: string; number: number | null; text: string }
  | { kind: "calls"; id: string; calls: AgentCall[] }
  | { kind: "messages"; id: string; texts: string[] }
  | { kind: "report"; id: string; number: number | null; text: string };

export interface AgentRun {
  /** The subagent's session id, or its call's `toolCallId` without one. */
  id: string;
  label: string;
  /** The kind of subagent, e.g. `Explore`. */
  type: string | null;
  /** The model it ran on, once its first response named it. */
  model: string | null;
  /** The subagent that started it; null for the chat's agent. */
  startedBy: string | null;
  /** How its latest run stands. */
  state: SubagentState;
  status: RunStatus;
  /** Why it failed, when the agent says. */
  error: string | null;
  /** Its working directory, when it is not the one of what started it. */
  cwd: string | null;
  /** It runs and the agent takes a stop for it. */
  canStop: boolean;
  /** When the run that is going now was seen to start (ms since epoch). */
  runningSince: number | null;
  /** How long its finished runs took together; null when none was seen. */
  ranMs: number | null;
  /** Everything it was told, did, said and reported, oldest first. */
  parts: AgentPart[];
  /** The subagents it started, oldest first. */
  agents: AgentRun[];
}

const SUBAGENT_TOOLS = new Set(["Agent", "Task"]);
/** Whether a call starts a subagent. */
export const isSubagentCall = (block: ToolBlock) =>
  block.toolName !== undefined && SUBAGENT_TOOLS.has(block.toolName);

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

const numberOf = (value: unknown, key: string) => {
  if (typeof value !== "object" || value === null) return null;
  const field = (value as Record<string, unknown>)[key];
  return typeof field === "number" && Number.isFinite(field) ? field : null;
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

const statusOf = (state: SubagentState): RunStatus =>
  isActive(state) ? "run" : state === "failed" ? "err" : "ok";

const callOf = (block: ToolBlock, live: boolean): AgentCall => ({
  id: block.id,
  ...describeCall(block),
  status:
    !toolEnded(block.status) && live
      ? "run"
      : block.status === "failed"
        ? "err"
        : "ok",
  tookMs: span(block.startedAt, block.endedAt),
});

type Step =
  | { kind: "prompt" | "report" | "message"; id: string; text: string }
  | { kind: "call"; id: string; call: AgentCall };

/**
 * What a subagent did, from its own blocks. A report it handed back is a
 * message of its own to its parent; one that handed nothing back answered
 * with its last text, which the parent got as `<prompt id>:reply` and the
 * subagent has no copy of. A message it got from a subagent it started
 * (`below`) is that one's report, not a prompt.
 */
function partsOf(
  child: Subagent,
  parentBlocks: readonly TranscriptBlock[],
  below: ReadonlySet<string | null>,
): AgentPart[] {
  const live = isActive(child.state);
  const own = new Set(
    child.blocks.flatMap((b) => (b.kind === "message" ? [b.messageId] : [])),
  );
  const replies = parentBlocks.filter(
    (b): b is MessageBlock =>
      b.kind === "message" &&
      !b.outgoing &&
      b.senderSessionId === child.sessionId &&
      !own.has(b.messageId),
  );
  const steps: Step[] = [];
  // The reply to a prompt closes what the subagent did for it. Its text is
  // the subagent's last message, which is then not shown twice.
  const reply = (promptId: string | null) => {
    const index = replies.findIndex(
      (b) => promptId === null || b.messageId.startsWith(`${promptId}:`),
    );
    if (index < 0) return;
    const [block] = replies.splice(index, 1);
    const last = steps[steps.length - 1];
    if (last?.kind === "message" && last.text === block.text.trim()) {
      steps.pop();
    }
    steps.push({ kind: "report", id: block.id, text: block.text });
  };
  let promptId: string | null = null;
  for (const block of child.blocks) {
    if (block.kind === "message") {
      if (below.has(block.senderSessionId)) continue;
      if (!block.outgoing) {
        if (promptId !== null) reply(promptId);
        promptId = block.messageId;
        steps.push({ kind: "prompt", id: block.id, text: block.text });
      } else if (block.recipientSessionId === child.parentSessionId) {
        steps.push({ kind: "report", id: block.id, text: block.text });
      }
    } else if (block.kind === "tool") {
      // A subagent it started is drawn under it, not among its calls.
      if (!isSubagentCall(block)) {
        steps.push({ kind: "call", id: block.id, call: callOf(block, live) });
      }
    } else if (block.kind === "agent" && block.text.trim() !== "") {
      steps.push({ kind: "message", id: block.id, text: block.text.trim() });
    }
  }
  if (promptId !== null) reply(promptId);
  while (replies.length > 0) reply(null);

  const count = (kind: Step["kind"]) =>
    steps.filter((step) => step.kind === kind).length;
  const numbered = { prompt: count("prompt") > 1, report: count("report") > 1 };
  const seen = { prompt: 0, report: 0 };
  const parts: AgentPart[] = [];
  for (const step of steps) {
    const last = parts[parts.length - 1];
    if (step.kind === "call") {
      if (last?.kind === "calls") last.calls.push(step.call);
      else parts.push({ kind: "calls", id: step.id, calls: [step.call] });
    } else if (step.kind === "message") {
      if (last?.kind === "messages") last.texts.push(step.text);
      else parts.push({ kind: "messages", id: step.id, texts: [step.text] });
    } else {
      const number = numbered[step.kind] ? ++seen[step.kind] : null;
      parts.push({ kind: step.kind, id: step.id, number, text: step.text });
    }
  }
  return parts;
}

/** The value a subagent's own option is set to, by the option's id. */
function optionOf(child: Subagent, id: string): string | null {
  const option = child.configOptions.find((entry) => entry.id === id);
  return option?.type === "select" ? currentName(option) : null;
}

/**
 * The subagents a session started, oldest first, each with the ones it
 * started. `blocks` are that session's, and `live` says whether a call of
 * its that has not ended is still running.
 */
function startedIn(
  sessionId: string | null,
  blocks: readonly TranscriptBlock[],
  subagents: readonly Subagent[],
  startedBy: string | null,
  live: (index: number) => boolean,
): AgentRun[] {
  const children = subagents.filter(
    (child) => child.parentSessionId === sessionId,
  );
  const prompts = sentIn(blocks);
  const placed = new Set<Subagent>();
  const runs: AgentRun[] = [];

  const fromChild = (child: Subagent) => {
    placed.add(child);
    const label = child.title ?? child.description ?? "subagent";
    const open = child.runs.find((run) => run.endedAt === null);
    const closed = child.runs.filter((run) => run.endedAt !== null);
    runs.push({
      id: child.sessionId,
      label,
      type: optionOf(child, "agent") ?? child.description ?? child.title,
      model: optionOf(child, "model"),
      startedBy,
      state: child.state,
      status: statusOf(child.state),
      error: child.error,
      cwd: child.cwd,
      canStop: child.canCancel && isActive(child.state),
      runningSince: open?.startedAt ?? null,
      ranMs:
        closed.length === 0
          ? null
          : closed.reduce(
              (sum, run) =>
                sum + (span(run.startedAt, run.endedAt ?? undefined) ?? 0),
              0,
            ),
      parts: partsOf(
        child,
        blocks,
        new Set(
          subagents
            .filter((entry) => entry.parentSessionId === child.sessionId)
            .map((entry) => entry.sessionId),
        ),
      ),
      agents: startedIn(child.sessionId, child.blocks, subagents, label, () =>
        isActive(child.state),
      ),
    });
  };

  // A subagent call that is still shown as a call has no session: all there
  // is to know of its subagent is on the call.
  const fromCall = (block: ToolBlock, runsNow: boolean) => {
    const input = block.rawInput;
    const prompt = stringField(input, "prompt");
    const result = resultOf(block);
    const state: SubagentState = runsNow
      ? "running"
      : block.status === "failed"
        ? "failed"
        : toolEnded(block.status)
          ? "finished"
          : "ended";
    const parts: AgentPart[] = [];
    if (prompt) {
      parts.push({
        kind: "prompt",
        id: `${block.id}:prompt`,
        number: null,
        text: prompt,
      });
    }
    if (result) {
      parts.push({
        kind: "report",
        id: `${block.id}:report`,
        number: null,
        text: result,
      });
    }
    runs.push({
      id: block.toolCallId,
      label: labelOf(block),
      type: stringField(input, "subagent_type"),
      model: stringField(input, "model"),
      startedBy,
      state,
      status: statusOf(state),
      error: null,
      cwd: null,
      canStop: false,
      runningSince: runsNow ? (block.startedAt ?? null) : null,
      ranMs: span(block.startedAt, block.endedAt),
      parts,
      agents: [],
    });
  };

  blocks.forEach((block, index) => {
    if (block.kind === "message" && block.outgoing) {
      const child = children.find(
        (entry) => entry.sessionId === block.recipientSessionId,
      );
      if (child && !placed.has(child)) fromChild(child);
      return;
    }
    // A call whose id is a prompt's was shown by a permission request, and
    // its subagent is announced too.
    if (
      block.kind === "tool" &&
      isSubagentCall(block) &&
      !prompts.has(block.toolCallId)
    ) {
      fromCall(block, !toolEnded(block.status) && live(index));
    }
  });
  children.forEach((child) => {
    if (!placed.has(child)) fromChild(child);
  });
  return runs;
}

/**
 * The chat's subagents, oldest first, each with the subagents it started.
 * `running` says a turn is live: only then is a subagent call that has not
 * ended still running.
 */
export function agentsOf(
  chat: { sessionId: string | null; blocks: readonly TranscriptBlock[] },
  subagents: readonly Subagent[],
  running: boolean,
): AgentRun[] {
  const live = liveFrom(chat.blocks, running);
  return startedIn(
    chat.sessionId,
    chat.blocks,
    subagents,
    null,
    (index) => index >= live,
  );
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
