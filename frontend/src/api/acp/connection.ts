import * as acp from "@agentclientprotocol/sdk";

import { openChannel } from "../linuxio";
import type { Stream } from "../StreamMultiplexer";
import {
  ASSISTANT_AGENTS,
  type AssistantAgentId,
  isAssistantAgentId,
} from "./agents";
import { type AssistantExit, createAcpTransport } from "./transport";

export { ASSISTANT_AGENTS, type AssistantAgentId, isAssistantAgentId };

/** Pushed by the Claude adapter as `_auth/status_update`; other agents stay silent. */
export interface AuthStatus {
  kind: "account" | "api_key" | "gateway" | "external" | "none";
  label: string;
  detail?: string;
  account?: { email?: string; organization?: string; plan?: string };
}

export interface AssistantHandlers {
  onUpdate(notification: acp.SessionNotification): void;
  onPermission(
    request: acp.RequestPermissionRequest,
  ): Promise<acp.RequestPermissionResponse>;
  onAuthStatus?(status: AuthStatus): void;
  onExit(exit: AssistantExit | null): void;
}

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

const defaultOpen = (agent: AssistantAgentId): Stream | null =>
  openChannel("assistant.open", { agent });

export async function connectAssistant(
  agent: AssistantAgentId,
  handlers: AssistantHandlers,
  open: (agent: AssistantAgentId) => Stream | null = defaultOpen,
): Promise<AssistantConnection> {
  const stream = open(agent);
  if (!stream) {
    throw new Error("Not connected to the server");
  }
  const transport = createAcpTransport(stream);
  void transport.exit.then((exit) => {
    handlers.onExit(exit);
  });

  const connection = acp
    .client({ name: "linuxio" })
    .onRequest(acp.methods.client.session.requestPermission, (ctx) =>
      handlers.onPermission(ctx.params),
    )
    .onNotification(acp.methods.client.session.update, (ctx) => {
      handlers.onUpdate(ctx.params);
    })
    .onNotification("_auth/status_update", parseAuthStatus, (ctx) => {
      handlers.onAuthStatus?.(ctx.params.authStatus);
    })
    .connect(transport.stream);

  let cwd: string;
  let init: acp.InitializeResponse;
  try {
    ({ cwd } = await transport.ready);
    init = await connection.agent.request(acp.methods.agent.initialize, {
      protocolVersion: acp.PROTOCOL_VERSION,
      // Boolean options (Fast mode) arrive as real toggles instead of an
      // "On"/"Off" select once the agent knows the client can show them.
      clientCapabilities: {
        auth: { terminal: true },
        session: { configOptions: { boolean: {} } },
      },
    });
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
        { sessionId, cwd, mcpServers: [] },
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
        { cwd, mcpServers: [] },
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
        { sessionId, cwd, mcpServers: [] },
      );
      const forked = {
        sessionId: result.sessionId,
        modes: result.modes ?? null,
        configOptions: result.configOptions ?? [],
      };
      if (!canResume) return forked;
      const resumed = await connection.agent.request(
        acp.methods.agent.session.resume,
        { sessionId: forked.sessionId, cwd, mcpServers: [] },
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
