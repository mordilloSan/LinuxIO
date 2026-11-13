/**
 * The subagents of a chat, read from the transcript the page already holds:
 * each subagent call, the calls its subagent made, and what it says it used.
 * Claude Code's adapter names the subagent call each call of a subagent was
 * made in; an agent that does not shows its subagents without their calls.
 */
import {
  stringField,
  type SubagentTotals,
  type ToolBlock,
  toolEnded,
  type TranscriptBlock,
} from "../transcript/transcript";
import { labelOf, liveFrom, span, stationOf, type WorkRow } from "./workMap";

export type RunStatus = "run" | "ok" | "err";

/** A call a subagent made, as a row of the activity feed. */
export type AgentCall = WorkRow & { status: RunStatus };

export interface AgentRun {
  /** The subagent call's `toolCallId`. */
  id: string;
  label: string;
  /** The kind of subagent, e.g. `Explore`, when the call names one. */
  type: string | null;
  /** The model it was asked to run on, when the call names one. */
  model: string | null;
  prompt: string | null;
  /** What it handed back, once it has. */
  result: string | null;
  /** Running while its call or any call it made still runs. */
  status: RunStatus;
  /** When it was seen to start (ms since epoch); null in a loaded chat. */
  startedAt: number | null;
  /** How long it took: its own account, else what was seen. */
  tookMs: number | null;
  /** Its own account of the tokens it used; a loaded chat has none. */
  tokens: number | null;
  /** Its own count of tool calls, else the calls seen. */
  toolUses: number;
  /** The calls it made, oldest first, without the subagents it started. */
  calls: AgentCall[];
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
  const reported = new Map<AgentRun, SubagentTotals | undefined>();
  const top: AgentRun[] = [];

  blocks.forEach((block, index) => {
    if (block.kind !== "tool") return;
    const status: RunStatus =
      !toolEnded(block.status) && index >= live
        ? "run"
        : block.status === "failed"
          ? "err"
          : "ok";
    const parent =
      block.parentId === undefined ? undefined : byCall.get(block.parentId);
    if (isSubagentCall(block)) {
      const input = block.rawInput;
      const agent: AgentRun = {
        id: block.toolCallId,
        label: labelOf(block),
        type: stringField(input, "subagent_type"),
        model: stringField(input, "model"),
        prompt: stringField(input, "prompt"),
        result: resultOf(block),
        status,
        startedAt: block.startedAt ?? null,
        tookMs:
          block.totals?.durationMs ?? span(block.startedAt, block.endedAt),
        tokens: block.totals?.tokens ?? null,
        toolUses: 0,
        calls: [],
        agents: [],
      };
      reported.set(agent, block.totals);
      byCall.set(block.toolCallId, agent);
      (parent?.agents ?? top).push(agent);
      return;
    }
    parent?.calls.push({
      id: block.id,
      station: stationOf(block),
      label: labelOf(block),
      status,
      times: 1,
      tookMs: span(block.startedAt, block.endedAt),
    });
  });

  // A subagent sent to the background hands back at once and works on, so it
  // runs while anything it started does.
  const settle = (agent: AgentRun) => {
    agent.agents.forEach(settle);
    if (
      agent.calls.some((call) => call.status === "run") ||
      agent.agents.some((child) => child.status === "run")
    ) {
      agent.status = "run";
    }
    agent.toolUses =
      reported.get(agent)?.toolUses ?? agent.calls.length + agent.agents.length;
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
