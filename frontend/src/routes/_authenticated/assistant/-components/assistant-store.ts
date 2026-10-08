import type * as acp from "@agentclientprotocol/sdk";
import { useSyncExternalStore } from "react";

import {
  type AssistantAgentId,
  type AssistantConnection,
  type AssistantExit,
  type AuthStatus,
  connectAssistant,
  isAuthRequiredError,
  type SessionOpenResult,
  textBlock,
} from "@/api/acp";

import {
  appendUserMessage,
  applySessionUpdate,
  type TranscriptBlock,
} from "./transcript";

export type AssistantStatus =
  | "idle"
  | "connecting"
  | "ready"
  | "running"
  | "auth_required"
  | "stopped"
  | "error";

export interface PendingPermission {
  request: acp.RequestPermissionRequest;
  resolve(response: acp.RequestPermissionResponse): void;
}

/** A forked chat for asking something without touching the main one. */
export interface SideSession {
  sessionId: string;
  blocks: TranscriptBlock[];
  status: "ready" | "running";
  pending: PendingPermission | null;
  open: boolean;
  error: string | null;
}

export interface AssistantState {
  agent: AssistantAgentId | null;
  status: AssistantStatus;
  blocks: TranscriptBlock[];
  sessions: acp.SessionInfo[];
  sessionId: string | null;
  pending: PendingPermission | null;
  error: string | null;
  exit: AssistantExit | null;
  authStatus: AuthStatus | null;
  authMethods: acp.AuthMethod[];
  canListSessions: boolean;
  canLoadSession: boolean;
  canSteer: boolean;
  canDeleteSession: boolean;
  canFork: boolean;
  /** A side question is being forked; the panel shows "Opening…" meanwhile. */
  sideOpening: boolean;
  promptCapabilities: { image: boolean; embeddedContext: boolean };
  configOptions: acp.SessionConfigOption[];
  modes: acp.SessionModeState | null;
  commands: acp.AvailableCommand[];
  usage: {
    used: number;
    size: number;
    cost: { amount: number; currency: string } | null;
  } | null;
  /** A steering message the agent could not inject; sent as a normal prompt once the running turn ends. */
  queuedPrompt: acp.ContentBlock[] | null;
  /** When the running main turn started (ms since epoch); null while idle. */
  turnStartedAt: number | null;
  side: SideSession | null;
}

/** What the user's message looks like in the transcript. */
export interface MessageDisplay {
  text: string;
  attachments: string[];
}

// Per-chat agent state, cleared whenever the open chat changes.
const sessionReset = (): Partial<AssistantState> => ({
  blocks: [],
  configOptions: [],
  modes: null,
  commands: [],
  usage: null,
  queuedPrompt: null,
  side: null,
});

const initialState = (): AssistantState => ({
  agent: null,
  status: "idle",
  blocks: [],
  sessions: [],
  sessionId: null,
  pending: null,
  error: null,
  exit: null,
  authStatus: null,
  authMethods: [],
  canListSessions: false,
  canLoadSession: false,
  canSteer: false,
  canDeleteSession: false,
  canFork: false,
  sideOpening: false,
  promptCapabilities: { image: false, embeddedContext: false },
  configOptions: [],
  modes: null,
  commands: [],
  usage: null,
  queuedPrompt: null,
  turnStartedAt: null,
  side: null,
});

let state = initialState();
let connection: AssistantConnection | null = null;
let connector: typeof connectAssistant = connectAssistant;
// Bumped by every connect() and disconnect(); handlers of a superseded
// connection compare against it and go quiet (a closed transport still emits
// onExit(null) from a microtask).
let currentAttempt = 0;
// Bumped whenever the side session is dropped; a fork still in flight compares
// against it so it never resurrects a side for a chat that is gone.
let sideEpoch = 0;
let forking = false;
// A /btw question typed while a fork is in flight; sent when the fork resolves.
let queuedSideText: string | null = null;
const SIDE_BUSY = "Side question is busy; try again when it finishes";
// Bumped by stop(); a steer still in flight compares against it so a
// stopped turn is not restarted by its late "promptRequired" answer.
let stopEpoch = 0;
const listeners = new Set<() => void>();

// Session updates arrive per token. Rendering the transcript for each one is
// what makes streaming look jerky, so they queue here and are applied in one
// state update per animation frame. Turn boundaries (prompt settled, session
// loaded, agent exited) flush synchronously so the state is complete when the
// status changes.
// Each carries its session so a late replay for another chat is dropped.
let pendingUpdates: acp.SessionNotification[] = [];
let flushHandle: number | ReturnType<typeof setTimeout> | null = null;

function flushUpdates() {
  if (flushHandle !== null) {
    if (
      typeof cancelAnimationFrame === "function" &&
      typeof flushHandle === "number"
    ) {
      cancelAnimationFrame(flushHandle);
    } else {
      clearTimeout(flushHandle as ReturnType<typeof setTimeout>);
    }
    flushHandle = null;
  }
  if (pendingUpdates.length === 0) return;
  const batch = pendingUpdates;
  pendingUpdates = [];
  const forSession = (id: string | undefined) =>
    batch.filter((n) => n.sessionId === id).map((n) => n.update);
  const updates = forSession(state.sessionId ?? undefined);
  const side = state.side;
  const sideUpdates = side ? forSession(side.sessionId) : [];
  if (updates.length === 0 && sideUpdates.length === 0) return;
  const patch = updates.reduce<Partial<AssistantState>>(
    (acc, update) => ({
      ...acc,
      ...statePatch(update, { ...state, ...acc }),
    }),
    {},
  );
  setState({
    ...patch,
    ...(updates.length > 0 && {
      blocks: updates.reduce(applySessionUpdate, state.blocks),
    }),
    ...(side &&
      sideUpdates.length > 0 && {
        side: {
          ...side,
          blocks: sideUpdates.reduce(applySessionUpdate, side.blocks),
        },
      }),
  });
}

// Updates that describe the chat rather than add to the transcript.
function statePatch(
  update: acp.SessionUpdate,
  current: AssistantState,
): Partial<AssistantState> {
  switch (update.sessionUpdate) {
    case "available_commands_update":
      return { commands: update.availableCommands };
    case "current_mode_update":
      return current.modes
        ? { modes: { ...current.modes, currentModeId: update.currentModeId } }
        : {};
    case "config_option_update":
      return { configOptions: update.configOptions };
    case "usage_update":
      return {
        usage: {
          used: update.used,
          size: update.size,
          cost: update.cost
            ? { amount: update.cost.amount, currency: update.cost.currency }
            : null,
        },
      };
    case "session_info_update": {
      const { sessionId } = current;
      if (!sessionId || typeof update.title !== "string") return {};
      const title = update.title;
      const known = current.sessions.some((s) => s.sessionId === sessionId);
      return {
        sessions: withoutSide(
          known
            ? current.sessions.map((s) =>
                s.sessionId === sessionId ? { ...s, title } : s,
              )
            : [
                { sessionId, cwd: connection?.cwd ?? "", title },
                ...current.sessions,
              ],
          current.side,
        ),
      };
    }
    default:
      return {};
  }
}

// A live side session is a throwaway fork; it never shows in the chat list.
const withoutSide = (sessions: acp.SessionInfo[], side: SideSession | null) =>
  side ? sessions.filter((s) => s.sessionId !== side.sessionId) : sessions;

// The Claude adapter titles forks "<title> (fork)".
const isFork = (session: acp.SessionInfo) =>
  session.title?.endsWith(" (fork)") === true;

function queueUpdate(notification: acp.SessionNotification) {
  pendingUpdates.push(notification);
  if (flushHandle !== null) return;
  flushHandle =
    typeof requestAnimationFrame === "function"
      ? requestAnimationFrame(flushUpdates)
      : setTimeout(flushUpdates, 0);
}

function setState(patch: Partial<AssistantState>) {
  state = { ...state, ...patch };
  listeners.forEach((listener) => listener());
}

const errorMessage = (error: unknown) =>
  error instanceof Error
    ? error.message
    : typeof error === "object" && error && "message" in error
      ? String(error.message)
      : String(error);

async function openSession(
  conn: AssistantConnection,
  isCurrent: () => boolean,
) {
  const sessions = await conn.listSessions();
  if (!isCurrent()) return;
  setState({
    sessions: withoutSide(sessions, state.side),
    canListSessions: conn.canListSessions,
    canLoadSession: conn.canLoadSession,
    canSteer: conn.canSteer,
    canDeleteSession: conn.canDeleteSession,
    canFork: conn.canFork,
    promptCapabilities: conn.promptCapabilities,
  });
  try {
    let opened: SessionOpenResult;
    if (sessions.length > 0 && conn.canLoadSession) {
      // Skip forks orphaned by a page reload unless they are all there is.
      const newest = sessions.find((s) => !isFork(s)) ?? sessions[0];
      setState({ ...sessionReset(), sessionId: newest.sessionId });
      opened = await conn.loadSession(newest.sessionId);
    } else {
      opened = await conn.newSession();
      if (!isCurrent()) return;
      setState({ ...sessionReset(), sessionId: opened.sessionId });
    }
    flushUpdates();
    // The open response is authoritative; replayed history may be older.
    if (isCurrent()) {
      setState({
        modes: opened.modes,
        configOptions: opened.configOptions,
        status: "ready",
      });
    }
  } catch (error) {
    // An exit already reported itself as "stopped"; don't turn it into "error".
    if (!isCurrent() || state.status === "stopped") return;
    if (isAuthRequiredError(error)) {
      setState({ status: "auth_required" });
      return;
    }
    throw error;
  }
}

// Runs one prompt to completion. A steering message the agent could not inject
// (queuedPrompt) goes out as the next turn once this one has settled; it is
// already in the transcript, so nothing is appended for it.
async function runTurn(
  conn: AssistantConnection,
  sessionId: string,
  blocks: acp.ContentBlock[],
) {
  const attempt = currentAttempt;
  const isCurrent = () => attempt === currentAttempt;
  try {
    await conn.prompt(sessionId, blocks);
  } catch (error) {
    if (!isCurrent()) return;
    if (isAuthRequiredError(error)) {
      setState({
        status: "auth_required",
        queuedPrompt: null,
        turnStartedAt: null,
      });
      return;
    }
    setState({ error: errorMessage(error) });
  } finally {
    if (isCurrent()) {
      flushUpdates();
      const queued = state.queuedPrompt;
      // getState(): TS narrows `state.status` after the guards in the callers.
      if (assistantStore.getState().status === "running") {
        setState({
          status: queued ? "running" : "ready",
          queuedPrompt: null,
          turnStartedAt: queued ? Date.now() : null,
        });
        if (queued) await runTurn(conn, sessionId, queued);
      } else {
        setState({ queuedPrompt: null, turnStartedAt: null });
      }
    }
  }
}

const cancelled = { outcome: { outcome: "cancelled" } } as const;

// Best effort: the panel does not wait for the agent, and errors are dropped.
async function releaseSide(conn: AssistantConnection, sessionId: string) {
  try {
    if (conn.canClose) await conn.closeSession(sessionId);
    if (conn.canDeleteSession) await conn.deleteSession(sessionId);
  } catch {
    // An orphaned fork is skipped when the next chat is chosen.
  }
}

// Drops the side session: answers its permission, then cancels it when the
// agent cannot close it, and removes the fork's transcript when it can.
function dropSide() {
  sideEpoch++;
  forking = false;
  if (state.sideOpening) setState({ sideOpening: false });
  const side = state.side;
  if (!side) return Promise.resolve();
  side.pending?.resolve(cancelled);
  const conn = connection;
  let released = Promise.resolve();
  if (conn) {
    if (side.status === "running" && !conn.canClose) {
      void conn.cancel(side.sessionId);
    }
    released = releaseSide(conn, side.sessionId);
  }
  setState({ side: null });
  return released;
}

export const assistantStore = {
  getState: () => state,
  subscribe: (listener: () => void) => {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  },

  async connect(agent: AssistantAgentId) {
    if (
      state.agent === agent &&
      (state.status === "connecting" ||
        (connection && state.status !== "stopped" && state.status !== "error"))
    ) {
      return;
    }
    assistantStore.disconnect();
    const attempt = currentAttempt;
    const isCurrent = () => attempt === currentAttempt;
    setState({ agent, status: "connecting" });
    let conn: AssistantConnection | undefined;
    try {
      conn = await connector(agent, {
        onUpdate: (notification) => {
          if (isCurrent()) queueUpdate(notification);
        },
        onPermission: (request) =>
          new Promise<acp.RequestPermissionResponse>((resolve) => {
            if (!isCurrent()) {
              resolve({ outcome: { outcome: "cancelled" } });
              return;
            }
            const pending = { request, resolve };
            if (state.side && request.sessionId === state.side.sessionId) {
              state.side.pending?.resolve(cancelled);
              setState({ side: { ...state.side, pending, open: true } });
            } else if (request.sessionId === state.sessionId) {
              state.pending?.resolve(cancelled);
              setState({ pending });
            } else {
              // A discarded side, a fork not yet registered, or another chat.
              resolve(cancelled);
            }
          }),
        onAuthStatus: (authStatus) => {
          if (isCurrent()) setState({ authStatus });
        },
        onExit: (exit) => {
          if (!isCurrent()) return;
          flushUpdates();
          connection = null;
          sideEpoch++;
          forking = false;
          state.pending?.resolve(cancelled);
          state.side?.pending?.resolve(cancelled);
          setState({
            sideOpening: false,
            status: "stopped",
            exit,
            pending: null,
            side: null,
            turnStartedAt: null,
            error: null,
          });
        },
      });
      if (!isCurrent()) {
        conn.close();
        return;
      }
      connection = conn;
      setState({ authMethods: conn.init.authMethods ?? [] });
      await openSession(conn, isCurrent);
    } catch (error) {
      if (!isCurrent()) return;
      conn?.close();
      connection = null;
      if (state.status === "stopped") return;
      setState({ status: "error", error: errorMessage(error) });
    }
  },

  async send(blocks: acp.ContentBlock[], display: MessageDisplay) {
    const conn = connection;
    const sessionId = state.sessionId;
    if (!conn || !sessionId || state.status !== "ready") return;
    setState({
      status: "running",
      turnStartedAt: Date.now(),
      blocks: appendUserMessage(
        state.blocks,
        display.text,
        display.attachments,
      ),
      error: null,
    });
    await runTurn(conn, sessionId, blocks);
  },

  async steer(blocks: acp.ContentBlock[], display: MessageDisplay) {
    const conn = connection;
    const sessionId = state.sessionId;
    if (!conn?.canSteer || !sessionId || state.status !== "running") return;
    setState({
      blocks: appendUserMessage(
        state.blocks,
        display.text,
        display.attachments,
      ),
    });
    const attempt = currentAttempt;
    const stopped = stopEpoch;
    try {
      const outcome = await conn.steer(sessionId, blocks);
      if (attempt !== currentAttempt || outcome !== "promptRequired") return;
      if (stopped !== stopEpoch) return;
      const current = assistantStore.getState();
      if (current.status === "ready" && current.sessionId === sessionId) {
        // The turn settled while steering was in flight: nothing will drain
        // the queue, so run it now. It is already in the transcript.
        setState({ status: "running", turnStartedAt: Date.now() });
        await runTurn(conn, sessionId, blocks);
      } else {
        setState({ queuedPrompt: [...(state.queuedPrompt ?? []), ...blocks] });
      }
    } catch (error) {
      if (attempt === currentAttempt) setState({ error: errorMessage(error) });
    }
  },

  async setConfigOption(configId: string, value: string | boolean) {
    const conn = connection;
    const sessionId = state.sessionId;
    if (!conn || !sessionId) return;
    const attempt = currentAttempt;
    try {
      const configOptions = await conn.setConfigOption(
        sessionId,
        configId,
        value,
      );
      if (attempt === currentAttempt && state.sessionId === sessionId) {
        // A queued config_option_update is older than this response.
        flushUpdates();
        setState({ configOptions });
      }
    } catch (error) {
      if (attempt === currentAttempt) setState({ error: errorMessage(error) });
    }
  },

  async setMode(modeId: string) {
    const conn = connection;
    const sessionId = state.sessionId;
    if (!conn || !sessionId) return;
    const attempt = currentAttempt;
    try {
      await conn.setMode(sessionId, modeId);
      if (attempt === currentAttempt && state.sessionId === sessionId) {
        flushUpdates();
        if (state.modes) {
          setState({ modes: { ...state.modes, currentModeId: modeId } });
        }
      }
    } catch (error) {
      if (attempt === currentAttempt) setState({ error: errorMessage(error) });
    }
  },

  async deleteSession(sessionId: string) {
    const conn = connection;
    if (!conn?.canDeleteSession) return;
    const isOpen = sessionId === state.sessionId;
    if (isOpen && state.status === "running") return;
    const attempt = currentAttempt;
    try {
      await conn.deleteSession(sessionId);
      if (attempt !== currentAttempt) return;
      setState({
        sessions: state.sessions.filter((s) => s.sessionId !== sessionId),
      });
      if (isOpen) await assistantStore.newSession();
    } catch (error) {
      if (attempt === currentAttempt) setState({ error: errorMessage(error) });
    }
  },

  stop() {
    stopEpoch++;
    const pending = state.pending;
    if (pending) {
      pending.resolve({ outcome: { outcome: "cancelled" } });
      setState({ pending: null });
    }
    if (state.queuedPrompt) setState({ queuedPrompt: null });
    if (connection && state.sessionId) {
      void connection.cancel(state.sessionId);
    }
  },

  answerPermission(optionId: string | null) {
    const pending = state.pending;
    if (!pending) return;
    pending.resolve(
      optionId === null
        ? { outcome: { outcome: "cancelled" } }
        : { outcome: { outcome: "selected", optionId } },
    );
    setState({ pending: null });
  },

  /** Opens the side panel (forking the chat if needed); `initialText` is sent as its first question. */
  async openSide(initialText?: string) {
    const conn = connection;
    const sessionId = state.sessionId;
    if (state.status !== "ready" && state.status !== "running") return;
    if (state.side) {
      const busy = Boolean(initialText) && state.side.status !== "ready";
      setState({
        side: {
          ...state.side,
          open: true,
          error: busy ? SIDE_BUSY : state.side.error,
        },
      });
      if (initialText && !busy) await assistantStore.sendSide(initialText);
      return;
    }
    if (!conn?.canFork || !sessionId) return;
    if (forking) {
      if (!initialText) return;
      // The first queued question wins; there is no side panel to show this in yet.
      if (queuedSideText) setState({ error: SIDE_BUSY });
      else queuedSideText = initialText;
      return;
    }
    forking = true;
    setState({ sideOpening: true });
    queuedSideText = initialText ?? null;
    const epoch = sideEpoch;
    try {
      const forked = await conn.forkSession(sessionId);
      if (epoch !== sideEpoch) {
        // The chat moved on while forking: release the orphan.
        if (conn.canClose) await conn.closeSession(forked.sessionId);
        return;
      }
      const side: SideSession = {
        sessionId: forked.sessionId,
        blocks: [],
        status: "ready",
        pending: null,
        open: true,
        error: null,
      };
      setState({ side, sessions: withoutSide(state.sessions, side) });
    } catch (error) {
      if (epoch === sideEpoch) setState({ error: errorMessage(error) });
      return;
    } finally {
      if (epoch === sideEpoch) {
        forking = false;
        setState({ sideOpening: false });
      }
    }
    const text = queuedSideText;
    queuedSideText = null;
    if (text) await assistantStore.sendSide(text);
  },

  async sendSide(text: string) {
    const conn = connection;
    const side = state.side;
    if (!conn || !side || side.status !== "ready") return;
    const { sessionId } = side;
    setState({
      side: {
        ...side,
        status: "running",
        error: null,
        blocks: appendUserMessage(side.blocks, text, []),
      },
    });
    let error: string | null = null;
    try {
      await conn.prompt(sessionId, [textBlock(text)]);
    } catch (e) {
      error = errorMessage(e);
    }
    flushUpdates();
    const current = state.side;
    if (current?.sessionId === sessionId) {
      setState({ side: { ...current, status: "ready", error } });
    }
  },

  stopSide() {
    const side = state.side;
    if (!side) return;
    if (side.pending) {
      side.pending.resolve(cancelled);
      setState({ side: { ...side, pending: null } });
    }
    void connection?.cancel(side.sessionId);
  },

  answerSidePermission(optionId: string | null) {
    const side = state.side;
    if (!side?.pending) return;
    side.pending.resolve(
      optionId === null
        ? cancelled
        : { outcome: { outcome: "selected", optionId } },
    );
    setState({ side: { ...side, pending: null } });
  },

  hideSide() {
    if (state.side) setState({ side: { ...state.side, open: false } });
  },

  discardSide() {
    return dropSide();
  },

  async selectSession(sessionId: string) {
    const conn = connection;
    if (!conn || state.status === "running" || state.status === "connecting") {
      return;
    }
    const attempt = currentAttempt;
    // A newer chat open supersedes this load, not just a reconnect.
    const isCurrent = () =>
      attempt === currentAttempt && state.sessionId === sessionId;
    pendingUpdates = [];
    void dropSide();
    setState({ ...sessionReset(), sessionId, status: "connecting" });
    try {
      const opened = await conn.loadSession(sessionId);
      if (isCurrent()) {
        flushUpdates();
        setState({
          modes: opened.modes,
          configOptions: opened.configOptions,
          status: "ready",
        });
      }
    } catch (error) {
      if (isCurrent()) {
        setState({ status: "error", error: errorMessage(error) });
      }
    }
  },

  async newSession() {
    const conn = connection;
    if (!conn || state.status === "running" || state.status === "connecting") {
      return;
    }
    const attempt = currentAttempt;
    const isCurrent = () => attempt === currentAttempt;
    void dropSide();
    try {
      const opened = await conn.newSession();
      if (!isCurrent()) return;
      setState({
        ...sessionReset(),
        sessionId: opened.sessionId,
        modes: opened.modes,
        configOptions: opened.configOptions,
        status: "ready",
      });
      const sessions = await conn.listSessions();
      if (isCurrent())
        setState({ sessions: withoutSide(sessions, state.side) });
    } catch (error) {
      if (!isCurrent()) return;
      setState({
        status: isAuthRequiredError(error) ? "auth_required" : "error",
        error: errorMessage(error),
      });
    }
  },

  async restart() {
    const agent = state.agent;
    assistantStore.disconnect();
    if (agent) await assistantStore.connect(agent);
  },

  disconnect() {
    currentAttempt++;
    pendingUpdates = [];
    flushUpdates();
    state.pending?.resolve(cancelled);
    void dropSide();
    connection?.close();
    connection = null;
    state = initialState();
    listeners.forEach((listener) => listener());
  },

  _setConnector(fn: typeof connectAssistant | null) {
    connector = fn ?? connectAssistant;
  },

  _patch(patch: Partial<AssistantState>) {
    setState(patch);
  },

  _flush() {
    flushUpdates();
  },
};

export function useAssistantState(): AssistantState {
  return useSyncExternalStore(
    assistantStore.subscribe,
    assistantStore.getState,
    assistantStore.getState,
  );
}
