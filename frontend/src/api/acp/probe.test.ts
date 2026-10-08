import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AssistantConnection,
  AssistantHandlers,
  AuthStatus,
  SessionOpenResult,
} from "./connection";
import {
  authenticateAssistant,
  logoutAssistant,
  probeAssistant,
} from "./probe";

type Connect = Parameters<typeof probeAssistant>[1];

function fakeConnect(
  overrides: Partial<AssistantConnection> = {},
  pushStatus?: AuthStatus,
) {
  const conn = {
    agent: "claude",
    cwd: "/home/alice",
    init: { protocolVersion: 1, authMethods: [] },
    canListSessions: false,
    canLoadSession: false,
    canLogout: true,
    newSession: vi.fn(async () => ({
      sessionId: "s-new",
      modes: null,
      configOptions: [],
    })),
    authenticate: vi.fn(async () => {}),
    logout: vi.fn(async () => {}),
    close: vi.fn(),
    ...overrides,
  } as unknown as AssistantConnection;
  const connect = vi.fn(async (_agent: string, handlers: AssistantHandlers) => {
    if (pushStatus) queueMicrotask(() => handlers.onAuthStatus?.(pushStatus));
    return conn;
  });
  return { conn, connect: connect as unknown as NonNullable<Connect> };
}

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

describe("probeAssistant", () => {
  it("closes the connection and reports cancelled when aborted mid-probe", async () => {
    const { conn, connect } = fakeConnect({
      newSession: vi.fn(() => new Promise<SessionOpenResult>(() => {})),
    });
    const controller = new AbortController();
    const pending = probeAssistant("claude", connect, controller.signal);
    await vi.advanceTimersByTimeAsync(2000);
    expect(conn.close).not.toHaveBeenCalled();
    controller.abort();
    expect(await pending).toMatchObject({
      status: "unknown",
      error: "cancelled",
    });
    expect(conn.close).toHaveBeenCalledTimes(1);
  });

  it("closes a connection that lands after the abort", async () => {
    const { conn } = fakeConnect();
    let land!: () => void;
    const connect = vi.fn(
      () =>
        new Promise<AssistantConnection>((resolve) => {
          land = () => resolve(conn);
        }),
    ) as unknown as NonNullable<Connect>;
    const controller = new AbortController();
    const pending = probeAssistant("claude", connect, controller.signal);
    controller.abort();
    expect(await pending).toMatchObject({ error: "cancelled" });
    land();
    await Promise.resolve();
    expect(conn.close).toHaveBeenCalledTimes(1);
  });

  it("reports logged_in from a pushed status without creating a session", async () => {
    const { conn, connect } = fakeConnect(
      {},
      { kind: "account", label: "Claude Max" },
    );
    const probe = await probeAssistant("claude", connect);
    expect(probe).toMatchObject({
      status: "logged_in",
      authStatus: { label: "Claude Max" },
      canLogout: true,
    });
    expect(conn.newSession).not.toHaveBeenCalled();
    expect(conn.close).toHaveBeenCalledTimes(1);
  });

  it("treats a pushed status of kind none as auth_required", async () => {
    const { conn, connect } = fakeConnect({}, { kind: "none", label: "" });
    const probe = await probeAssistant("claude", connect);
    expect(probe.status).toBe("auth_required");
    expect(conn.close).toHaveBeenCalledTimes(1);
  });

  it("falls back to newSession and reads auth_required from its error", async () => {
    const methods = [{ id: "api-key", name: "API key" }];
    const { conn, connect } = fakeConnect({
      init: { protocolVersion: 1, authMethods: methods },
      newSession: vi.fn(async () => {
        throw { code: -32000, message: "Authentication required" };
      }),
    });
    const pending = probeAssistant("claude", connect);
    await vi.advanceTimersByTimeAsync(2000);
    const probe = await pending;
    expect(probe).toMatchObject({
      status: "auth_required",
      authStatus: null,
      authMethods: methods,
    });
    expect(conn.close).toHaveBeenCalledTimes(1);
  });

  it("is logged_in when newSession succeeds without a pushed status", async () => {
    const { conn, connect } = fakeConnect();
    const pending = probeAssistant("claude", connect);
    await vi.advanceTimersByTimeAsync(2000);
    const probe = await pending;
    expect(probe).toMatchObject({ status: "logged_in", authStatus: null });
    expect(conn.close).toHaveBeenCalledTimes(1);
  });

  it("reports unknown instead of throwing when connecting fails", async () => {
    const connect = vi.fn(async () => {
      throw new Error("spawn failed");
    }) as unknown as NonNullable<Connect>;
    await expect(probeAssistant("claude", connect)).resolves.toMatchObject({
      status: "unknown",
      error: "spawn failed",
      canLogout: false,
    });
  });
});

describe("logoutAssistant / authenticateAssistant", () => {
  it("logs out and closes the connection", async () => {
    const { conn, connect } = fakeConnect();
    await logoutAssistant("claude", connect);
    expect(conn.logout).toHaveBeenCalledTimes(1);
    expect(conn.close).toHaveBeenCalledTimes(1);
  });

  it("authenticates with the method id and closes the connection", async () => {
    const { conn, connect } = fakeConnect();
    await authenticateAssistant("codex", "api-key", connect);
    expect(conn.authenticate).toHaveBeenCalledWith("api-key");
    expect(conn.close).toHaveBeenCalledTimes(1);
  });

  it("closes the connection even when authenticate rejects", async () => {
    const { conn, connect } = fakeConnect({
      authenticate: vi.fn(async () => {
        throw new Error("bad key");
      }),
    });
    await expect(
      authenticateAssistant("codex", "api-key", connect),
    ).rejects.toThrow("bad key");
    expect(conn.close).toHaveBeenCalledTimes(1);
  });
});
