/**
 * The subagents of the open chat, as the agent reports them: each one is a
 * session of its own, announced and patched with `subagent_update` on the
 * session that started it, and its calls and text arrive as ordinary updates
 * on its own session id. Only an agent that was told the client takes them
 * sends any (`clientCapabilities.subagents`), which today is Claude Code's
 * adapter.
 */
import type * as acp from "@agentclientprotocol/sdk/unstable";

import {
  applySessionUpdate,
  type TranscriptBlock,
} from "./transcript/transcript";

/**
 * How a subagent's work stands. `ended` is an end the agent gives no reason
 * for, and `disconnected` a subagent the agent can no longer see.
 */
export type SubagentState =
  | "running"
  | "waiting"
  | "finished"
  | "stopped"
  | "failed"
  | "ended"
  | "disconnected";

export interface Subagent {
  sessionId: string;
  /** The session that started it: the chat's, or another subagent's. */
  parentSessionId: string;
  title: string | null;
  /** Its role, when that is not already the title. */
  description: string | null;
  /** Its working directory, when it is not the one of what started it. */
  cwd: string | null;
  state: SubagentState;
  /** Why it failed, when the agent says. */
  error: string | null;
  /** The agent takes a `session/cancel` for it. */
  canCancel: boolean;
  /** Its own options, which cannot be set: mode, model, effort, agent. */
  configOptions: acp.SessionConfigOption[];
  blocks: TranscriptBlock[];
  /**
   * Each time it was seen to run (ms since epoch), oldest first; the last one
   * has no end while it runs. A subagent that is messaged again runs again.
   * Empty in a chat that was loaded.
   */
  runs: { startedAt: number; endedAt: number | null }[];
}

/** Whether a subagent is at work, waiting for the user included. */
export const isActive = (state: SubagentState) =>
  state === "running" || state === "waiting";

function stateOf(
  state: acp.StateUpdate | null,
): Pick<Subagent, "state" | "error"> {
  const none = { error: null };
  switch (state?.state) {
    case "running":
      return { state: "running", ...none };
    case "requires_action":
      return { state: "waiting", ...none };
    case "idle": {
      const idle = state as acp.IdleStateUpdate & acp.ErrorStopReason;
      if (idle.stopReason === "end_turn") return { state: "finished", ...none };
      if (idle.stopReason === "cancelled") return { state: "stopped", ...none };
      if (idle.stopReason === "error") {
        return { state: "failed", error: idle.error?.message ?? null };
      }
      return { state: "ended", ...none };
    }
    default:
      return { state: "disconnected", ...none };
  }
}

/** The runs after a change of state seen at `now`; unseen changes add none. */
function runsAfter(
  runs: Subagent["runs"],
  next: SubagentState,
  now: number | undefined,
): Subagent["runs"] {
  const last = runs[runs.length - 1];
  const open = last !== undefined && last.endedAt === null;
  if (isActive(next)) {
    return open || now === undefined
      ? runs
      : [...runs, { startedAt: now, endedAt: null }];
  }
  // A run that was seen to start ends when its end is seen.
  return open
    ? [...runs.slice(0, -1), { ...last, endedAt: now ?? Date.now() }]
    : runs;
}

/** A field of a patch: left out keeps the value, `null` clears it. */
const patched = <T>(next: T | null | undefined, held: T | null) =>
  next === undefined ? held : next;

function announced(
  list: readonly Subagent[],
  parentSessionId: string,
  update: acp.SubagentUpdate,
  now: number | undefined,
): Subagent[] {
  const index = list.findIndex((child) => child.sessionId === update.sessionId);
  const current = index < 0 ? null : list[index];
  const state =
    update.state === undefined && current
      ? { state: current.state, error: current.error }
      : stateOf(update.state ?? null);
  const child: Subagent = {
    sessionId: update.sessionId,
    parentSessionId: current?.parentSessionId ?? parentSessionId,
    title: patched(update.title, current?.title ?? null),
    description: patched(update.description, current?.description ?? null),
    cwd: patched(update.cwd, current?.cwd ?? null),
    ...state,
    canCancel:
      update.capabilities === undefined
        ? (current?.canCancel ?? false)
        : Boolean(update.capabilities?.cancel),
    configOptions: current?.configOptions ?? [],
    blocks: current?.blocks ?? [],
    runs: runsAfter(current?.runs ?? [], state.state, now),
  };
  return index < 0
    ? [...list, child]
    : [...list.slice(0, index), child, ...list.slice(index + 1)];
}

/**
 * One update that is not the chat's own: a `subagent_update` that came on
 * `sessionId` (the chat's or a known subagent's, which makes that session
 * the parent), or anything else a known subagent's session sent. `now` is the
 * time of an update that arrives while a turn runs.
 */
export function applySubagentUpdate(
  list: readonly Subagent[],
  sessionId: string,
  update: acp.SessionUpdate,
  now?: number,
): Subagent[] {
  if (update.sessionUpdate === "subagent_update") {
    return announced(list, sessionId, update, now);
  }
  const index = list.findIndex((child) => child.sessionId === sessionId);
  if (index < 0) return list as Subagent[];
  const child = list[index];
  // Its options and commands are its own: they never reach the chat's.
  const next =
    update.sessionUpdate === "config_option_update"
      ? { ...child, configOptions: update.configOptions }
      : {
          ...child,
          blocks: applySessionUpdate(child.blocks, update, now, sessionId),
        };
  if (
    next.blocks === child.blocks &&
    next.configOptions === child.configOptions
  )
    return list as Subagent[];
  return [...list.slice(0, index), next, ...list.slice(index + 1)];
}

/** The ids of the messages a session sent, from its blocks. */
export const sentIn = (blocks: readonly TranscriptBlock[]) =>
  new Set(
    blocks.flatMap((b) =>
      b.kind === "message" && b.outgoing ? [b.messageId] : [],
    ),
  );

/**
 * What a subagent did for one message: its blocks from its copy of the
 * message to the next message it got. `sent` holds the ids of the messages
 * the sender's session shows, so that work for a message it does not show
 * stays with the one before.
 */
export function workFor(
  child: Subagent,
  messageId: string,
  sent: ReadonlySet<string>,
): TranscriptBlock[] {
  const start = child.blocks.findIndex(
    (b) => b.kind === "message" && !b.outgoing && b.messageId === messageId,
  );
  if (start < 0) return [];
  const rest = child.blocks.slice(start + 1);
  const end = rest.findIndex(
    (b) => b.kind === "message" && !b.outgoing && sent.has(b.messageId),
  );
  return end < 0 ? rest : rest.slice(0, end);
}

/** The subagents once the agent is gone: none runs, and none can be stopped. */
export function disconnected(list: readonly Subagent[]): Subagent[] {
  return list.map((child) =>
    isActive(child.state)
      ? {
          ...child,
          state: "disconnected",
          canCancel: false,
          runs: runsAfter(child.runs, "disconnected", undefined),
        }
      : { ...child, canCancel: false },
  );
}
