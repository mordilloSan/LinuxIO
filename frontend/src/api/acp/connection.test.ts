import { describe, expect, it, vi } from "vitest";

import type { Stream } from "../StreamMultiplexer";
import {
  AGENT_START_TIMEOUT_MS,
  type AssistantHandlers,
  connectAssistant,
  isAuthRequiredError,
  textBlock,
} from "./connection";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

/** A fake bridge+agent: answers JSON-RPC requests from a table, records what was sent. */
function fakeAgent(
  answers: Record<string, (params: unknown, id: number) => unknown>,
) {
  const sent: Array<Record<string, unknown>> = [];
  let buffer = "";
  const stream = {
    abort: () => {},
    close: () => {
      stream.onClose?.();
    },
    id: 3,
    onClose: null,
    onData: null,
    onProgress: null,
    onResult: null,
    resize: () => {},
    status: "open",
    type: "assistant.open",
    write: (data: Uint8Array) => {
      buffer += decoder.decode(data);
      let nl = buffer.indexOf("\n");
      while (nl >= 0) {
        const line = buffer.slice(0, nl);
        buffer = buffer.slice(nl + 1);
        const message = JSON.parse(line) as {
          id?: number;
          method?: string;
          params?: unknown;
        };
        sent.push(message);
        if (message.method && message.id !== undefined) {
          const answer = answers[message.method];
          const reply = answer
            ? {
                jsonrpc: "2.0",
                id: message.id,
                result: answer(message.params, message.id),
              }
            : {
                jsonrpc: "2.0",
                id: message.id,
                error: { code: -32601, message: "no such method" },
              };
          queueMicrotask(() => feed(`${JSON.stringify(reply)}\n`));
        }
        nl = buffer.indexOf("\n");
      }
    },
  } as unknown as Stream & {
    onData: ((data: Uint8Array) => void) | null;
    onClose: (() => void) | null;
  };
  const feed = (text: string) => stream.onData?.(encoder.encode(text));
  const open = () => {
    queueMicrotask(() => feed('{"linuxio":"ready","cwd":"/home/alice"}\n'));
    return stream;
  };
  return { feed, open, sent, stream };
}

const handlers = () => ({
  onExit: vi.fn(),
  onPermission: vi.fn(async () => ({
    outcome: { outcome: "cancelled" as const },
  })),
  onUpdate: vi.fn(),
});

describe("connectAssistant", () => {
  it("initializes and reports capabilities", async () => {
    const agent = fakeAgent({
      initialize: () => ({
        protocolVersion: 1,
        agentCapabilities: {
          loadSession: true,
          sessionCapabilities: { list: {} },
          auth: { logout: {} },
        },
        authMethods: [],
      }),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    expect(conn.cwd).toBe("/home/alice");
    expect(conn.canListSessions).toBe(true);
    expect(conn.canLoadSession).toBe(true);
    expect(conn.canLogout).toBe(true);
    const init = agent.sent.find((m) => m.method === "initialize");
    expect(init?.params).toMatchObject({
      clientCapabilities: {
        auth: { terminal: true },
        session: { configOptions: { boolean: {} } },
      },
    });
  });

  it("lists sessions for the cwd newest first", async () => {
    const agent = fakeAgent({
      initialize: () => ({
        protocolVersion: 1,
        agentCapabilities: { sessionCapabilities: { list: {} } },
      }),
      "session/list": () => ({
        sessions: [
          {
            sessionId: "old",
            cwd: "/home/alice",
            updatedAt: "2026-10-01T00:00:00Z",
          },
          {
            sessionId: "elsewhere",
            cwd: "/srv",
            updatedAt: "2026-10-09T00:00:00Z",
          },
          {
            sessionId: "new",
            cwd: "/home/alice",
            updatedAt: "2026-10-07T00:00:00Z",
          },
        ],
      }),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    const sessions = await conn.listSessions();
    expect(sessions.map((s) => s.sessionId)).toEqual(["new", "old"]);
    expect(agent.sent.find((m) => m.method === "session/list")?.params).toEqual(
      { cwd: "/home/alice" },
    );
  });

  it("creates a session and sends a prompt as a text block", async () => {
    const agent = fakeAgent({
      initialize: () => ({ protocolVersion: 1 }),
      "session/new": () => ({ sessionId: "s1" }),
      "session/prompt": () => ({ stopReason: "end_turn" }),
    });
    const conn = await connectAssistant("gemini", handlers(), agent.open);
    expect(await conn.newSession()).toEqual({
      sessionId: "s1",
      modes: null,
      configOptions: [],
    });
    const result = await conn.prompt("s1", [textBlock("hello")]);
    expect(result.stopReason).toBe("end_turn");
    expect(
      agent.sent.find((m) => m.method === "session/prompt")?.params,
    ).toEqual({
      sessionId: "s1",
      prompt: [{ type: "text", text: "hello" }],
    });
  });

  it("reports steering, delete and prompt capabilities from initialize", async () => {
    const agent = fakeAgent({
      initialize: () => ({
        protocolVersion: 1,
        agentCapabilities: {
          promptCapabilities: { image: true, embeddedContext: true },
          sessionCapabilities: { delete: {} },
        },
        _meta: { steering: { supported: true } },
      }),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    expect(conn.canSteer).toBe(true);
    expect(conn.canDeleteSession).toBe(true);
    expect(conn.promptCapabilities).toEqual({
      image: true,
      embeddedContext: true,
    });
    const bare = fakeAgent({ initialize: () => ({ protocolVersion: 1 }) });
    const plain = await connectAssistant("gemini", handlers(), bare.open);
    expect(plain.canSteer).toBe(false);
    expect(plain.canDeleteSession).toBe(false);
    expect(plain.promptCapabilities).toEqual({
      image: false,
      embeddedContext: false,
    });
  });

  it("returns modes and config options from session/new and session/load", async () => {
    const modes = {
      currentModeId: "default",
      availableModes: [{ id: "default", name: "Default" }],
    };
    const configOptions = [
      {
        id: "model",
        name: "Model",
        type: "select",
        currentValue: "a",
        options: [{ value: "a", name: "A" }],
      },
    ];
    const agent = fakeAgent({
      initialize: () => ({ protocolVersion: 1 }),
      "session/new": () => ({ sessionId: "s1", modes, configOptions }),
      "session/load": () => ({ modes, configOptions }),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    expect(await conn.newSession()).toEqual({
      sessionId: "s1",
      modes,
      configOptions,
    });
    expect(await conn.loadSession("s9")).toEqual({
      sessionId: "s9",
      modes,
      configOptions,
    });
  });

  it("appends the LinuxIO instructions for an agent that takes a system prompt", async () => {
    const agent = fakeAgent({
      initialize: () => ({
        protocolVersion: 1,
        agentCapabilities: {
          sessionCapabilities: { fork: {}, resume: {}, systemPrompt: {} },
        },
      }),
      "session/new": () => ({ sessionId: "s1" }),
      "session/load": () => ({}),
      "session/fork": () => ({ sessionId: "s2" }),
      "session/resume": () => ({}),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    await conn.newSession();
    await conn.loadSession("s1");
    await conn.forkSession("s1");
    for (const method of [
      "session/new",
      "session/load",
      "session/fork",
      "session/resume",
    ]) {
      const params = agent.sent.find((m) => m.method === method)?.params as {
        systemPrompt?: { mode: string; content: { text: string }[] };
      };
      expect(params.systemPrompt?.mode, method).toBe("append");
      expect(params.systemPrompt?.content[0].text, method).toContain(
        "tagged `bash`",
      );
    }
  });

  it("sends no system prompt to an agent that does not advertise it", async () => {
    const agent = fakeAgent({
      initialize: () => ({ protocolVersion: 1 }),
      "session/new": () => ({ sessionId: "s1" }),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    await conn.newSession();
    expect(agent.sent.find((m) => m.method === "session/new")?.params).toEqual({
      cwd: "/home/alice",
      mcpServers: [],
    });
  });

  it("sends image and resource blocks verbatim", async () => {
    const agent = fakeAgent({
      initialize: () => ({ protocolVersion: 1 }),
      "session/prompt": () => ({ stopReason: "end_turn" }),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    const blocks = [
      { type: "image" as const, data: "AAAA", mimeType: "image/png" },
      textBlock("look"),
    ];
    await conn.prompt("s1", blocks);
    expect(
      agent.sent.find((m) => m.method === "session/prompt")?.params,
    ).toEqual({ sessionId: "s1", prompt: blocks });
  });

  it("steers through the _session/steering request", async () => {
    const agent = fakeAgent({
      initialize: () => ({ protocolVersion: 1 }),
      "_session/steering": () => ({ outcome: "injected" }),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    expect(await conn.steer("s1", [textBlock("also")])).toBe("injected");
    expect(
      agent.sent.find((m) => m.method === "_session/steering")?.params,
    ).toEqual({ sessionId: "s1", prompt: [{ type: "text", text: "also" }] });
  });

  it("sets config options and modes, and deletes sessions", async () => {
    const configOptions = [
      { id: "think", name: "Think", type: "boolean", currentValue: true },
    ];
    const agent = fakeAgent({
      initialize: () => ({ protocolVersion: 1 }),
      "session/set_config_option": () => ({ configOptions }),
      "session/set_mode": () => ({}),
      "session/delete": () => ({}),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    expect(await conn.setConfigOption("s1", "model", "b")).toEqual(
      configOptions,
    );
    await conn.setConfigOption("s1", "think", true);
    await conn.setMode("s1", "plan");
    await conn.deleteSession("s1");
    const params = (method: string) =>
      agent.sent.filter((m) => m.method === method).map((m) => m.params);
    expect(params("session/set_config_option")).toEqual([
      { sessionId: "s1", configId: "model", value: "b" },
      { sessionId: "s1", configId: "think", type: "boolean", value: true },
    ]);
    expect(params("session/set_mode")).toEqual([
      { sessionId: "s1", modeId: "plan" },
    ]);
    expect(params("session/delete")).toEqual([{ sessionId: "s1" }]);
  });

  it("forks and closes sessions when the agent advertises them", async () => {
    const agent = fakeAgent({
      initialize: () => ({
        protocolVersion: 1,
        agentCapabilities: { sessionCapabilities: { fork: {}, close: {} } },
      }),
      "session/fork": () => ({
        sessionId: "s2",
        modes: { currentModeId: "ask", availableModes: [] },
        configOptions: [
          { id: "think", name: "Think", type: "boolean", currentValue: true },
        ],
      }),
      "session/close": () => ({}),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    expect(conn.canFork).toBe(true);
    expect(conn.canClose).toBe(true);
    const forked = await conn.forkSession("s1");
    expect(forked).toMatchObject({
      sessionId: "s2",
      modes: { currentModeId: "ask" },
      configOptions: [{ id: "think" }],
    });
    await conn.closeSession("s2");
    const params = (method: string) =>
      agent.sent.filter((m) => m.method === method).map((m) => m.params);
    expect(params("session/fork")).toEqual([
      { sessionId: "s1", cwd: "/home/alice", mcpServers: [] },
    ]);
    expect(params("session/close")).toEqual([{ sessionId: "s2" }]);
  });

  it("resumes the fork when the agent advertises resume, preferring its modes and options", async () => {
    const agent = fakeAgent({
      initialize: () => ({
        protocolVersion: 1,
        agentCapabilities: { sessionCapabilities: { fork: {}, resume: {} } },
      }),
      "session/fork": () => ({ sessionId: "s2", modes: null }),
      "session/resume": () => ({
        modes: { currentModeId: "plan", availableModes: [] },
        configOptions: [
          { id: "think", name: "Think", type: "boolean", currentValue: true },
        ],
      }),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    expect(conn.canResume).toBe(true);
    const forked = await conn.forkSession("s1");
    expect(forked).toMatchObject({
      sessionId: "s2",
      modes: { currentModeId: "plan" },
      configOptions: [{ id: "think" }],
    });
    const methods = agent.sent
      .map((m) => m.method)
      .filter((m) => m === "session/fork" || m === "session/resume");
    expect(methods).toEqual(["session/fork", "session/resume"]);
    expect(
      agent.sent.find((m) => m.method === "session/resume")?.params,
    ).toEqual({ sessionId: "s2", cwd: "/home/alice", mcpServers: [] });
  });

  it("only forks when the agent cannot resume", async () => {
    const agent = fakeAgent({
      initialize: () => ({
        protocolVersion: 1,
        agentCapabilities: { sessionCapabilities: { fork: {} } },
      }),
      "session/fork": () => ({ sessionId: "s2" }),
    });
    const conn = await connectAssistant("claude", handlers(), agent.open);
    expect(conn.canResume).toBe(false);
    await conn.forkSession("s1");
    expect(agent.sent.some((m) => m.method === "session/resume")).toBe(false);
  });

  it("reports no fork or close, and closeSession is a no-op, without the capabilities", async () => {
    const agent = fakeAgent({ initialize: () => ({ protocolVersion: 1 }) });
    const conn = await connectAssistant("gemini", handlers(), agent.open);
    expect(conn.canFork).toBe(false);
    expect(conn.canClose).toBe(false);
    await conn.closeSession("s1");
    expect(agent.sent.some((m) => m.method === "session/close")).toBe(false);
  });

  it("routes session updates, permission requests and auth status to handlers", async () => {
    const agent = fakeAgent({ initialize: () => ({ protocolVersion: 1 }) });
    const h = { ...handlers(), onAuthStatus: vi.fn() };
    await connectAssistant("codex", h, agent.open);
    agent.feed(
      '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}}}\n',
    );
    agent.feed(
      '{"jsonrpc":"2.0","method":"_auth/status_update","params":{"authStatus":{"kind":"account","label":"Claude Max"}}}\n',
    );
    agent.feed(
      '{"jsonrpc":"2.0","id":99,"method":"session/request_permission","params":{"sessionId":"s1","toolCall":{"toolCallId":"t1","title":"Run ls"},"options":[{"optionId":"o1","name":"Allow","kind":"allow_once"}]}}\n',
    );
    await vi.waitFor(() => expect(h.onUpdate).toHaveBeenCalledTimes(1));
    await vi.waitFor(() =>
      expect(h.onAuthStatus).toHaveBeenCalledWith({
        kind: "account",
        label: "Claude Max",
      }),
    );
    await vi.waitFor(() => expect(h.onPermission).toHaveBeenCalledTimes(1));
    await vi.waitFor(() =>
      expect(
        agent.sent.find((m) => m.id === 99 && "result" in m),
      ).toMatchObject({ result: { outcome: { outcome: "cancelled" } } }),
    );
  });

  it("opens without panes unless a pane handler is given", async () => {
    const agent = fakeAgent({ initialize: () => ({ protocolVersion: 1 }) });
    const open = vi.fn(agent.open);
    await connectAssistant("claude", handlers(), open);
    expect(open).toHaveBeenCalledWith("claude", { panes: false });
  });

  it("opens with panes for a pane handler and lets it answer on the stream", async () => {
    const agent = fakeAgent({ initialize: () => ({ protocolVersion: 1 }) });
    const open = vi.fn(agent.open);
    const onPane = vi.fn<NonNullable<AssistantHandlers["onPane"]>>();
    await connectAssistant("claude", { ...handlers(), onPane }, open);
    expect(open).toHaveBeenCalledWith("claude", { panes: true });
    agent.feed('{"conn":4,"event":"open","linuxio":"pane"}\n');
    expect(onPane).toHaveBeenCalledWith(
      { conn: 4, event: "open", linuxio: "pane" },
      expect.any(Function),
    );
    const send = onPane.mock.calls[0][1];
    send(4, "p1", { subtype: "ui_panes", client_id: "linuxio-x" });
    expect(agent.sent.at(-1)).toEqual({
      linuxio: "pane",
      conn: 4,
      id: "p1",
      request: { subtype: "ui_panes", client_id: "linuxio-x" },
    });
  });

  it("reports the exit line through onExit", async () => {
    const agent = fakeAgent({ initialize: () => ({ protocolVersion: 1 }) });
    const h = handlers();
    await connectAssistant("claude", h, agent.open);
    agent.feed('{"linuxio":"exit","code":1,"stderr":"nope"}\n');
    agent.stream.close();
    await vi.waitFor(() =>
      expect(h.onExit).toHaveBeenCalledWith({ code: 1, stderr: "nope" }),
    );
  });

  it("recognises the ACP auth_required error", () => {
    expect(
      isAuthRequiredError({ code: -32000, message: "Authentication required" }),
    ).toBe(true);
    expect(isAuthRequiredError(new Error("x"))).toBe(false);
  });

  it("closes the stream and rejects when the handshake fails", async () => {
    const agent = fakeAgent({});
    const closeSpy = vi.spyOn(agent.stream, "close");
    const open = () => {
      queueMicrotask(() => agent.stream.close());
      return agent.stream;
    };
    await expect(connectAssistant("claude", handlers(), open)).rejects.toThrow(
      /closed before the agent was ready/,
    );
    expect(closeSpy).toHaveBeenCalled();
  });

  it("gives up and closes the stream when the agent never answers initialize", async () => {
    vi.useFakeTimers();
    try {
      const agent = fakeAgent({});
      // The bridge reports ready, then the agent stays silent.
      agent.stream.write = () => {};
      const closeSpy = vi.spyOn(agent.stream, "close");
      const outcome = connectAssistant("claude", handlers(), agent.open).then(
        () => "connected",
        (error: Error) => error.message,
      );
      await vi.advanceTimersByTimeAsync(AGENT_START_TIMEOUT_MS - 1);
      expect(closeSpy).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(1);
      expect(await outcome).toBe("The agent did not start within 30 seconds");
      expect(closeSpy).toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it("gives up when the bridge never reports the agent as started", async () => {
    vi.useFakeTimers();
    try {
      const agent = fakeAgent({});
      const outcome = connectAssistant(
        "claude",
        handlers(),
        () => agent.stream,
      ).then(
        () => "connected",
        (error: Error) => error.message,
      );
      await vi.advanceTimersByTimeAsync(AGENT_START_TIMEOUT_MS);
      expect(await outcome).toBe("The agent did not start within 30 seconds");
    } finally {
      vi.useRealTimers();
    }
  });
});
