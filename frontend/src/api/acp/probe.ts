import type * as acp from "@agentclientprotocol/sdk";

import {
  type AssistantAgentId,
  type AuthStatus,
  connectAssistant,
  isAuthRequiredError,
  type SessionOpenResult,
} from "./connection";

/** What a fresh chat offers: models, effort levels and permission modes. */
export type ChatOptions = Pick<SessionOpenResult, "configOptions" | "modes">;

export interface AssistantProbe {
  status: "logged_in" | "auth_required" | "unknown";
  authStatus: AuthStatus | null;
  authMethods: acp.AuthMethod[];
  canLogout: boolean;
  /** Read from a throwaway chat when logged in; null when none could open. */
  chatOptions: ChatOptions | null;
  error?: string;
}

const chatOptions = ({ configOptions, modes }: SessionOpenResult) => ({
  configOptions,
  modes,
});

const STATUS_WAIT_MS = 2000;

/** Races work against the signal; a pending connect is closed once it lands. */
function abortable(signal: AbortSignal | undefined) {
  const cancelled = new Promise<never>((_, reject) => {
    const fail = () => reject(new Error("cancelled"));
    if (signal?.aborted) fail();
    else signal?.addEventListener("abort", fail, { once: true });
  });
  cancelled.catch(() => {});
  return <T>(work: Promise<T>) => Promise.race([work, cancelled]);
}

function connectGuarded(
  connect: typeof connectAssistant,
  agent: AssistantAgentId,
  handlers: Parameters<typeof connectAssistant>[1],
  signal: AbortSignal | undefined,
  guard: ReturnType<typeof abortable>,
) {
  const connecting = connect(agent, handlers);
  // If the signal fires while connecting, nobody owns the connection once it lands.
  void connecting.then(
    (connection) => {
      if (signal?.aborted) connection.close();
    },
    () => {},
  );
  return guard(connecting);
}

/**
 * Opens the agent just long enough to learn how it is logged in and what a
 * chat offers. The Claude adapter pushes `_auth/status_update`; other agents
 * are probed by trying to create a session and reading the auth_required
 * error. Either way a logged-in probe opens a session for its options.
 */
export async function probeAssistant(
  agent: AssistantAgentId,
  connect: typeof connectAssistant = connectAssistant,
  signal?: AbortSignal,
): Promise<AssistantProbe> {
  const guard = abortable(signal);
  let authStatus: AuthStatus | null = null;
  let resolveStatus: (() => void) | null = null;
  const statusSeen = new Promise<void>((resolve) => {
    resolveStatus = resolve;
  });
  let connection: Awaited<ReturnType<typeof connect>> | null = null;
  try {
    connection = await connectGuarded(
      connect,
      agent,
      {
        onUpdate: () => {},
        onPermission: async () => ({ outcome: { outcome: "cancelled" } }),
        onAuthStatus: (status) => {
          authStatus = status;
          resolveStatus?.();
        },
        onExit: () => {},
      },
      signal,
      guard,
    );
    const authMethods = connection.init.authMethods ?? [];
    await guard(
      Promise.race([
        statusSeen,
        new Promise((resolve) => setTimeout(resolve, STATUS_WAIT_MS)),
      ]),
    );
    if (authStatus) {
      const loggedIn = (authStatus as AuthStatus).kind !== "none";
      return {
        status: loggedIn ? "logged_in" : "auth_required",
        authStatus,
        authMethods,
        canLogout: connection.canLogout,
        // The login answer stands even when no chat opens.
        chatOptions: loggedIn
          ? await guard(connection.newSession()).then(chatOptions, () => null)
          : null,
      };
    }
    try {
      const opened = await guard(connection.newSession());
      return {
        status: "logged_in",
        authStatus: null,
        authMethods,
        canLogout: connection.canLogout,
        chatOptions: chatOptions(opened),
      };
    } catch (error) {
      if (isAuthRequiredError(error)) {
        return {
          status: "auth_required",
          authStatus: null,
          authMethods,
          canLogout: connection.canLogout,
          chatOptions: null,
        };
      }
      throw error;
    }
  } catch (error) {
    return {
      status: "unknown",
      authStatus: null,
      authMethods: connection?.init.authMethods ?? [],
      canLogout: false,
      chatOptions: null,
      error: error instanceof Error ? error.message : String(error),
    };
  } finally {
    connection?.close();
  }
}

async function withConnection(
  agent: AssistantAgentId,
  connect: typeof connectAssistant,
  signal: AbortSignal | undefined,
  run: (connection: Awaited<ReturnType<typeof connect>>) => Promise<void>,
): Promise<void> {
  const guard = abortable(signal);
  const connection = await connectGuarded(
    connect,
    agent,
    {
      onUpdate: () => {},
      onPermission: async () => ({ outcome: { outcome: "cancelled" } }),
      onExit: () => {},
    },
    signal,
    guard,
  );
  try {
    await guard(run(connection));
  } finally {
    connection.close();
  }
}

export function logoutAssistant(
  agent: AssistantAgentId,
  connect: typeof connectAssistant = connectAssistant,
  signal?: AbortSignal,
): Promise<void> {
  return withConnection(agent, connect, signal, (connection) =>
    connection.logout(),
  );
}

/** For agent-handled auth methods (no `type: "terminal"`), e.g. an API key the agent reads from its own environment. */
export function authenticateAssistant(
  agent: AssistantAgentId,
  methodId: string,
  connect: typeof connectAssistant = connectAssistant,
  signal?: AbortSignal,
): Promise<void> {
  return withConnection(agent, connect, signal, (connection) =>
    connection.authenticate(methodId),
  );
}
