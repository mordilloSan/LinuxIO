import * as acp from "@agentclientprotocol/sdk/unstable";

import { openChannel } from "../linuxio";
import type { Stream } from "../StreamMultiplexer";
import {
  ASSISTANT_AGENTS,
  type AssistantAgentId,
  isAssistantAgentId,
} from "./agents";
import { withTimeout } from "./timeout";
import {
  type AssistantExit,
  createAcpTransport,
  type PaneLine,
  type PaneSend,
} from "./transport";

export { ASSISTANT_AGENTS, type AssistantAgentId, isAssistantAgentId };

/** Pushed by the Claude adapter as `_auth/status_update`; other agents stay silent. */
export interface AuthStatus {
  kind: "account" | "api_key" | "gateway" | "external" | "none";
  label: string;
  detail?: string;
  account?: { email?: string; organization?: string; plan?: string };
}

/**
 * Appended to the agent's own instructions when it advertises
 * `sessionCapabilities.systemPrompt`. The transcript offers "Open in
 * terminal" only on a fence tagged as shell, so an untagged command can
 * only be copied.
 */
const LINUXIO_SYSTEM_PROMPT: acp.SystemPrompt = {
  mode: "append",
  content: [
    {
      type: "text",
      text: [
        "You are running inside the LinuxIO web interface, which shows your answers as Markdown.",
        'When you give the user a shell command to run themselves, put it in a fenced code block tagged `bash`. LinuxIO adds an "Open in terminal" button to those blocks, which types the block into the user\'s terminal for them to run.',
        "Keep such a block to the commands alone: no prompt characters such as `$`, and no output. Put command output, logs and file contents in a separate block with their own language tag, or none.",
      ].join("\n\n"),
    },
  ],
};

export interface AssistantHandlers {
  onUpdate(notification: acp.SessionNotification): void;
  /**
   * `withdrawn` aborts when the agent takes the request back, which is not an
   * answer: Claude Code does when it stops the work that asked. The agent no
   * longer waits then, and drops whatever is answered.
   */
  onPermission(
    request: acp.RequestPermissionRequest,
    withdrawn?: AbortSignal,
  ): Promise<acp.RequestPermissionResponse>;
  onAuthStatus?(status: AuthStatus): void;
  onExit(exit: AssistantExit | null): void;
  /**
   * Plugin pane lines from Claude Code; `send` answers on the same stream.
   * Only a handler that sets this opens the stream with panes on.
   */
  onPane?: (line: PaneLine, send: PaneSend) => void;
}

/** Opens the agent's duplex stream; `panes` asks the bridge to carry plugin panes. */
export type AssistantOpen = (
  agent: AssistantAgentId,
  options: { panes: boolean },
) => Stream | null;

export interface SessionOpenResult {
  sessionId: string;
  modes: acp.SessionModeState | null;
  configOptions: acp.SessionConfigOption[];
}

export type SteerOutcome = "injected" | "startedNewTurn" | "promptRequired";

export const textBlock = (text: string): acp.ContentBlock => ({
  type: "text",
  text,
});

export interface AssistantConnection {
  agent: AssistantAgentId;
  cwd: string;
  init: acp.InitializeResponse;
  canListSessions: boolean;
  canLoadSession: boolean;
  canLogout: boolean;
  canSteer: boolean;
  canDeleteSession: boolean;
  canFork: boolean;
  canClose: boolean;
  canResume: boolean;
  promptCapabilities: { image: boolean; embeddedContext: boolean };
  listSessions(): Promise<acp.SessionInfo[]>;
  loadSession(sessionId: string): Promise<SessionOpenResult>;
  newSession(): Promise<SessionOpenResult>;
  prompt(
    sessionId: string,
    blocks: acp.ContentBlock[],
  ): Promise<acp.PromptResponse>;
  steer(sessionId: string, blocks: acp.ContentBlock[]): Promise<SteerOutcome>;
  setConfigOption(
    sessionId: string,
    configId: string,
    value: string | boolean,
  ): Promise<acp.SessionConfigOption[]>;
  setMode(sessionId: string, modeId: string): Promise<void>;
  deleteSession(sessionId: string): Promise<void>;
  /**
   * Forks a session into a new one that starts with its history. Some agents
   * (Claude) return the fork unregistered, so it is resumed before use.
   */
  forkSession(sessionId: string): Promise<SessionOpenResult>;
  /** Releases a session on the agent; a no-op when the agent cannot. */
  closeSession(sessionId: string): Promise<void>;
  cancel(sessionId: string): Promise<void>;
  authenticate(methodId: string): Promise<void>;
  logout(): Promise<void>;
  close(): void;
}

const AUTH_REQUIRED_CODE = -32000;

/**
 * How long an agent gets to start and answer `initialize`. The bridge runs
 * an agent that is already installed and never downloads one, so a healthy
 * start takes a second or two; past this the caller is told instead of left
 * waiting.
 */
export const AGENT_START_TIMEOUT_MS = 30_000;

export function isAuthRequiredError(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    error.code === AUTH_REQUIRED_CODE
  );
}

const parseAuthStatus = (params: unknown): { authStatus: AuthStatus } =>
  params as { authStatus: AuthStatus };

const newestFirst = (a: acp.SessionInfo, b: acp.SessionInfo) =>
  (b.updatedAt ?? "").localeCompare(a.updatedAt ?? "");

const defaultOpen: AssistantOpen = (agent, { panes }) =>
  openChannel("assistant.open", panes ? { agent, panes } : { agent });

export async function connectAssistant(
  agent: AssistantAgentId,
  handlers: AssistantHandlers,
  open: AssistantOpen = defaultOpen,
): Promise<AssistantConnection> {
  const { onPane } = handlers;
  const stream = open(agent, { panes: Boolean(onPane) });
  if (!stream) {
    throw new Error("Not connected to the server");
  }
  const transport = createAcpTransport(
    stream,
    onPane ? { onPane: (line) => onPane(line, transport.sendPane) } : {},
  );
  void transport.exit.then((exit) => {
    handlers.onExit(exit);
  });

  const connection = acp
    .client({ name: "linuxio" })
    .onRequest(acp.methods.client.session.requestPermission, (ctx) =>
      handlers.onPermission(ctx.params, ctx.signal),
    )
    .onNotification(acp.methods.client.session.update, (ctx) => {
      handlers.onUpdate(ctx.params);
    })
    .onNotification("_auth/status_update", parseAuthStatus, (ctx) => {
      handlers.onAuthStatus?.(ctx.params.authStatus);
    })
    .connect(transport.stream);

  const handshake = async () => {
    const { cwd } = await transport.ready;
    const init = await connection.agent.request(acp.methods.agent.initialize, {
      protocolVersion: acp.PROTOCOL_VERSION,
      // Boolean options (Fast mode) arrive as real toggles instead of an
      // "On"/"Off" select once the agent knows the client can show them.
      // With `subagents`, an agent that has them sends each one as a session
      // of its own (the store keeps them); the others ignore it.
      clientCapabilities: {
        auth: { terminal: true },
        session: { configOptions: { boolean: {} } },
        subagents: {},
      },
    });
    return { cwd, init };
  };
  let cwd: string;
  let init: acp.InitializeResponse;
  try {
    ({ cwd, init } = await withTimeout(
      handshake(),
      AGENT_START_TIMEOUT_MS,
      `The agent did not start within ${AGENT_START_TIMEOUT_MS / 1000} seconds`,
    ));
  } catch (error) {
    connection.close();
    transport.close();
    throw error;
  }
  const caps = init.agentCapabilities ?? {};
  const steering = (init._meta as { steering?: { supported?: boolean } } | null)
    ?.steering;
  const canListSessions = Boolean(caps.sessionCapabilities?.list);
  const canClose = Boolean(caps.sessionCapabilities?.close);
  const canResume = Boolean(caps.sessionCapabilities?.resume);
  // Sent with every request that starts or reopens a session: an agent
  // rebuilds its instructions each time, so a reopened chat would lose them.
  const sessionSetup: Pick<
    acp.NewSessionRequest,
    "cwd" | "mcpServers" | "systemPrompt"
  > = {
    cwd,
    mcpServers: [],
    ...(caps.sessionCapabilities?.systemPrompt
      ? { systemPrompt: LINUXIO_SYSTEM_PROMPT }
      : {}),
  };

  return {
    agent,
    cwd,
    init,
    canListSessions,
    canLoadSession: Boolean(caps.loadSession),
    canLogout: Boolean(caps.auth?.logout),
    canSteer: steering?.supported === true,
    canDeleteSession: Boolean(caps.sessionCapabilities?.delete),
    canFork: Boolean(caps.sessionCapabilities?.fork),
    canClose,
    canResume,
    promptCapabilities: {
      image: Boolean(caps.promptCapabilities?.image),
      embeddedContext: Boolean(caps.promptCapabilities?.embeddedContext),
    },
    async listSessions() {
      if (!canListSessions) return [];
      const result = await connection.agent.request(
        acp.methods.agent.session.list,
        { cwd },
      );
      return result.sessions.filter((s) => s.cwd === cwd).sort(newestFirst);
    },
    async loadSession(sessionId) {
      const result = await connection.agent.request(
        acp.methods.agent.session.load,
        { sessionId, ...sessionSetup },
      );
      return {
        sessionId,
        modes: result.modes ?? null,
        configOptions: result.configOptions ?? [],
      };
    },
    async newSession() {
      const result = await connection.agent.request(
        acp.methods.agent.session.new,
        { ...sessionSetup },
      );
      return {
        sessionId: result.sessionId,
        modes: result.modes ?? null,
        configOptions: result.configOptions ?? [],
      };
    },
    prompt(sessionId, blocks) {
      return connection.agent.request(acp.methods.agent.session.prompt, {
        sessionId,
        prompt: blocks,
      });
    },
    async steer(sessionId, blocks) {
      const result = await connection.agent.request<
        { outcome: SteerOutcome },
        { sessionId: string; prompt: acp.ContentBlock[] }
      >("_session/steering", { sessionId, prompt: blocks });
      return result.outcome;
    },
    async setConfigOption(sessionId, configId, value) {
      const result = await connection.agent.request(
        acp.methods.agent.session.setConfigOption,
        typeof value === "boolean"
          ? { sessionId, configId, type: "boolean", value }
          : { sessionId, configId, value },
      );
      return result.configOptions;
    },
    async setMode(sessionId, modeId) {
      await connection.agent.request(acp.methods.agent.session.setMode, {
        sessionId,
        modeId,
      });
    },
    async deleteSession(sessionId) {
      await connection.agent.request(acp.methods.agent.session.delete, {
        sessionId,
      });
    },
    async forkSession(sessionId) {
      const result = await connection.agent.request(
        acp.methods.agent.session.fork,
        { sessionId, ...sessionSetup },
      );
      const forked = {
        sessionId: result.sessionId,
        modes: result.modes ?? null,
        configOptions: result.configOptions ?? [],
      };
      if (!canResume) return forked;
      const resumed = await connection.agent.request(
        acp.methods.agent.session.resume,
        { sessionId: forked.sessionId, ...sessionSetup },
      );
      return {
        sessionId: forked.sessionId,
        modes: resumed.modes ?? forked.modes,
        configOptions: resumed.configOptions ?? forked.configOptions,
      };
    },
    async closeSession(sessionId) {
      if (!canClose) return;
      await connection.agent.request(acp.methods.agent.session.close, {
        sessionId,
      });
    },
    cancel(sessionId) {
      return connection.agent.notify(acp.methods.agent.session.cancel, {
        sessionId,
      });
    },
    async authenticate(methodId) {
      await connection.agent.request(acp.methods.agent.authenticate, {
        methodId,
      });
    },
    async logout() {
      await connection.agent.request(acp.methods.agent.logout, {});
    },
    close() {
      connection.close();
      transport.close();
    },
  };
}
