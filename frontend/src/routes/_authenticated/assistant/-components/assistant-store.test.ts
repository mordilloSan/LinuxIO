import type * as acp from "@agentclientprotocol/sdk";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  type AssistantConnection,
  type AssistantHandlers,
  type SessionOpenResult,
  textBlock,
} from "@/api/acp";

import { assistantStore } from "./assistant-store";

function fakeConnection(overrides: Partial<AssistantConnection> = {}) {
  let handlers!: AssistantHandlers;
  const conn: AssistantConnection = {
    agent: "claude",
    cwd: "/home/alice",
    init: { protocolVersion: 1, authMethods: [] },
    canListSessions: true,
    canLoadSession: true,
    canLogout: false,
    canSteer: false,
    canDeleteSession: false,
    canFork: false,
    canClose: false,
    canResume: false,
    promptCapabilities: { image: false, embeddedContext: false },
    listSessions: vi.fn(async () => [
      { sessionId: "s-old", cwd: "/home/alice", title: "Old chat" },
    ]),
    loadSession: vi.fn(async (sessionId: string) => ({
      sessionId,
      modes: null,
      configOptions: [],
    })),
    newSession: vi.fn(async () => ({
      sessionId: "s-new",
      modes: null,
      configOptions: [],
    })),
    prompt: vi.fn(async () => ({ stopReason: "end_turn" as const })),
    steer: vi.fn(async () => "injected" as const),
    setConfigOption: vi.fn(async () => []),
    setMode: vi.fn(async () => {}),
    deleteSession: vi.fn(async () => {}),
    forkSession: vi.fn(async () => ({
      sessionId: "s-side",
      modes: null,
      configOptions: [],
    })),
    closeSession: vi.fn(async () => {}),
    cancel: vi.fn(async () => {}),
    authenticate: vi.fn(async () => {}),
    logout: vi.fn(async () => {}),
    close: vi.fn(),
    ...overrides,
  };
  const connector = vi.fn(async (_agent, h: AssistantHandlers) => {
    handlers = h;
    return conn;
  });
  return { conn, connector, handlers: () => handlers };
}

const send = (text: string) =>
  assistantStore.send([textBlock(text)], { text, attachments: [] });

const selectOption = (value: string) => ({
  id: "model",
  name: "Model",
  type: "select" as const,
  currentValue: value,
  options: [
    { value: "a", name: "A" },
    { value: "b", name: "B" },
  ],
});

beforeEach(() => {
  assistantStore.disconnect();
});

describe("assistantStore", () => {
  it("connects, loads the newest session and becomes ready", async () => {
    const { conn, connector } = fakeConnection();
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    expect(conn.loadSession).toHaveBeenCalledWith("s-old");
    expect(assistantStore.getState()).toMatchObject({
      status: "ready",
      sessionId: "s-old",
      sessions: [{ sessionId: "s-old" }],
    });
  });

  it("creates a session when there is no history", async () => {
    const { conn, connector } = fakeConnection({
      listSessions: vi.fn(async () => []),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    expect(conn.newSession).toHaveBeenCalled();
    expect(assistantStore.getState().sessionId).toBe("s-new");
  });

  it("marks auth_required when session creation needs a login", async () => {
    const { connector } = fakeConnection({
      listSessions: vi.fn(async () => []),
      newSession: vi.fn(async () => {
        throw { code: -32000, message: "Authentication required" };
      }),
      init: {
        protocolVersion: 1,
        authMethods: [
          {
            id: "claude-login",
            name: "Log in",
            type: "terminal",
            args: ["--cli"],
          },
        ],
      },
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    expect(assistantStore.getState()).toMatchObject({
      status: "auth_required",
      authMethods: [{ id: "claude-login" }],
    });
  });

  it("sends a prompt, records blocks and returns to ready", async () => {
    const { conn, connector, handlers } = fakeConnection();
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const sending = send("hello");
    expect(assistantStore.getState().status).toBe("running");
    handlers().onUpdate({
      sessionId: "s-old",
      update: {
        sessionUpdate: "agent_message_chunk",
        content: { type: "text", text: "hi" },
      },
    });
    await sending;
    expect(conn.prompt).toHaveBeenCalledWith("s-old", [textBlock("hello")]);
    expect(assistantStore.getState().blocks.map((b) => b.kind)).toEqual([
      "user",
      "agent",
    ]);
    expect(assistantStore.getState().status).toBe("ready");
  });

  it("applies streamed chunks in one state update per frame, and flushes at turn end", async () => {
    let finishTurn!: () => void;
    const { connector, handlers } = fakeConnection({
      prompt: vi.fn(
        () =>
          new Promise<{ stopReason: "end_turn" }>((resolve) => {
            finishTurn = () => resolve({ stopReason: "end_turn" });
          }),
      ),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const listener = vi.fn();
    const unsubscribe = assistantStore.subscribe(listener);
    const sending = send("hello");
    listener.mockClear();
    for (const text of ["a", "b", "c"]) {
      handlers().onUpdate({
        sessionId: "s-old",
        update: {
          sessionUpdate: "agent_message_chunk",
          content: { type: "text", text },
        },
      });
    }
    // Nothing rendered yet: three chunks, zero notifications.
    expect(listener).not.toHaveBeenCalled();
    expect(assistantStore.getState().blocks.map((b) => b.kind)).toEqual([
      "user",
    ]);
    await vi.waitFor(() => expect(listener).toHaveBeenCalledTimes(1));
    expect(assistantStore.getState().blocks).toEqual([
      expect.objectContaining({ kind: "user" }),
      expect.objectContaining({ kind: "agent", text: "abc" }),
    ]);
    // A chunk that lands right before the prompt settles is in the state
    // when the status flips to ready.
    handlers().onUpdate({
      sessionId: "s-old",
      update: {
        sessionUpdate: "agent_message_chunk",
        content: { type: "text", text: "d" },
      },
    });
    finishTurn();
    await sending;
    expect(assistantStore.getState()).toMatchObject({ status: "ready" });
    expect(assistantStore.getState().blocks[1]).toMatchObject({ text: "abcd" });
    unsubscribe();
  });

  it("holds a permission request until answered, even with no subscriber", async () => {
    const { connector, handlers } = fakeConnection();
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const unsubscribe = assistantStore.subscribe(() => {});
    unsubscribe();
    const answer = handlers().onPermission({
      sessionId: "s-old",
      toolCall: { toolCallId: "t1", title: "Run ls" },
      options: [{ optionId: "yes", name: "Allow", kind: "allow_once" }],
    });
    expect(assistantStore.getState().pending?.request.toolCall.title).toBe(
      "Run ls",
    );
    assistantStore.answerPermission("yes");
    await expect(answer).resolves.toEqual({
      outcome: { outcome: "selected", optionId: "yes" },
    });
    expect(assistantStore.getState().pending).toBeNull();
  });

  it("stop cancels the turn and answers a pending permission with cancelled", async () => {
    const { conn, connector, handlers } = fakeConnection();
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    void send("x");
    const answer = handlers().onPermission({
      sessionId: "s-old",
      toolCall: { toolCallId: "t", title: "rm" },
      options: [],
    });
    assistantStore.stop();
    expect(conn.cancel).toHaveBeenCalledWith("s-old");
    await expect(answer).resolves.toEqual({
      outcome: { outcome: "cancelled" },
    });
  });

  it("records the exit line when the agent stops", async () => {
    const { connector, handlers } = fakeConnection();
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    handlers().onExit({ code: 1, stderr: "npx: not found" });
    expect(assistantStore.getState()).toMatchObject({
      status: "stopped",
      exit: { code: 1, stderr: "npx: not found" },
    });
  });

  it("selectSession replaces the transcript with the loaded one", async () => {
    const { conn, connector, handlers } = fakeConnection({
      listSessions: vi.fn(async () => [
        { sessionId: "a", cwd: "/home/alice" },
        { sessionId: "b", cwd: "/home/alice" },
      ]),
      loadSession: vi.fn(async (id: string) => {
        handlers().onUpdate({
          sessionId: id,
          update: {
            sessionUpdate: "user_message_chunk",
            content: { type: "text", text: id },
          },
        });
        return { sessionId: id, modes: null, configOptions: [] };
      }),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    await assistantStore.selectSession("b");
    expect(conn.loadSession).toHaveBeenLastCalledWith("b");
    expect(assistantStore.getState().blocks).toEqual([
      expect.objectContaining({ kind: "user", text: "b" }),
    ]);
  });

  it("ignores onExit(null) from a closed connection when restarting", async () => {
    const connector = vi.fn(async (_agent, h: AssistantHandlers) => {
      const { conn } = fakeConnection({
        // The real transport resolves `exit` with null from a microtask on close.
        close: vi.fn(() => {
          queueMicrotask(() => h.onExit(null));
        }),
      });
      return conn;
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const seen: string[] = [];
    const unsubscribe = assistantStore.subscribe(() => {
      seen.push(assistantStore.getState().status);
    });
    await assistantStore.restart();
    await Promise.resolve();
    unsubscribe();
    expect(seen).not.toContain("stopped");
    expect(assistantStore.getState()).toMatchObject({
      status: "ready",
      exit: null,
    });
  });

  it("does not start a second connection while one is connecting", async () => {
    const { conn, connector } = fakeConnection();
    let release!: () => void;
    const held = vi.fn(async (...args: Parameters<typeof connector>) => {
      await new Promise<void>((resolve) => {
        release = resolve;
      });
      return connector(...args);
    });
    assistantStore._setConnector(held);
    const first = assistantStore.connect("claude");
    const second = assistantStore.connect("claude");
    release();
    await Promise.all([first, second]);
    expect(held).toHaveBeenCalledTimes(1);
    expect(conn.close).not.toHaveBeenCalled();
    expect(assistantStore.getState().status).toBe("ready");
  });

  it("keeps stopped with the stderr tail when the agent exits during initialize", async () => {
    const connector = vi.fn(async (_agent, h: AssistantHandlers) => {
      h.onExit({ code: 127, stderr: "npx: not found" });
      throw new Error("Connection closed");
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    expect(assistantStore.getState()).toMatchObject({
      status: "stopped",
      exit: { code: 127, stderr: "npx: not found" },
      error: null,
    });
  });

  it("does not write a superseded prompt's failure onto a fresh connection", async () => {
    let rejectPrompt!: (error: Error) => void;
    const first = fakeConnection({
      prompt: vi.fn(
        () =>
          new Promise<never>((_resolve, reject) => {
            rejectPrompt = reject;
          }),
      ),
    });
    assistantStore._setConnector(first.connector);
    await assistantStore.connect("claude");
    const sending = send("hello");

    const second = fakeConnection();
    assistantStore._setConnector(second.connector);
    await assistantStore.connect("gemini");
    rejectPrompt(new Error("transport closed"));
    await sending;

    expect(assistantStore.getState()).toMatchObject({
      agent: "gemini",
      status: "ready",
      error: null,
    });
  });

  it("stores modes and config options from session/new", async () => {
    const modes = {
      currentModeId: "default",
      availableModes: [{ id: "default", name: "Default" }],
    };
    const { connector } = fakeConnection({
      listSessions: vi.fn(async () => []),
      newSession: vi.fn(async () => ({
        sessionId: "s-new",
        modes,
        configOptions: [selectOption("a")],
      })),
      canSteer: true,
      canDeleteSession: true,
      promptCapabilities: { image: true, embeddedContext: true },
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    expect(assistantStore.getState()).toMatchObject({
      modes,
      configOptions: [{ id: "model", currentValue: "a" }],
      canSteer: true,
      canDeleteSession: true,
      promptCapabilities: { image: true, embeddedContext: true },
    });
  });

  it("applies chat-state updates to store fields, not the transcript", async () => {
    const { connector, handlers } = fakeConnection({
      listSessions: vi.fn(async () => []),
      newSession: vi.fn(async () => ({
        sessionId: "s-new",
        modes: {
          currentModeId: "default",
          availableModes: [
            { id: "default", name: "Default" },
            { id: "plan", name: "Plan" },
          ],
        },
        configOptions: [selectOption("a")],
      })),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const updates = [
      {
        sessionUpdate: "config_option_update",
        configOptions: [selectOption("b")],
      },
      { sessionUpdate: "current_mode_update", currentModeId: "plan" },
      {
        sessionUpdate: "available_commands_update",
        availableCommands: [{ name: "compact", description: "Compact" }],
      },
      {
        sessionUpdate: "usage_update",
        used: 1200,
        size: 200000,
        cost: { amount: 0.42, currency: "USD" },
      },
      { sessionUpdate: "session_info_update", title: "Renamed" },
    ] as acp.SessionUpdate[];
    for (const update of updates) {
      handlers().onUpdate({ sessionId: "s-new", update });
    }
    assistantStore._flush();
    const state = assistantStore.getState();
    expect(state.configOptions).toMatchObject([
      { id: "model", currentValue: "b" },
    ]);
    expect(state.modes?.currentModeId).toBe("plan");
    expect(state.commands).toEqual([
      { name: "compact", description: "Compact" },
    ]);
    expect(state.usage).toEqual({
      used: 1200,
      size: 200000,
      cost: { amount: 0.42, currency: "USD" },
    });
    expect(state.sessions).toMatchObject([
      { sessionId: "s-new", title: "Renamed" },
    ]);
    expect(state.blocks).toEqual([]);
  });

  it("renames the matching session on session_info_update", async () => {
    const { connector, handlers } = fakeConnection();
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    handlers().onUpdate({
      sessionId: "s-old",
      update: { sessionUpdate: "session_info_update", title: "Nice title" },
    });
    assistantStore._flush();
    expect(assistantStore.getState().sessions).toMatchObject([
      { sessionId: "s-old", title: "Nice title" },
    ]);
  });

  it("steer injected appends a user block and keeps the turn running", async () => {
    let finishTurn!: () => void;
    const { conn, connector } = fakeConnection({
      canSteer: true,
      prompt: vi.fn(
        () =>
          new Promise<{ stopReason: "end_turn" }>((resolve) => {
            finishTurn = () => resolve({ stopReason: "end_turn" });
          }),
      ),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const sending = send("first");
    await assistantStore.steer([textBlock("also")], {
      text: "also",
      attachments: ["a.txt"],
    });
    expect(conn.steer).toHaveBeenCalledWith("s-old", [textBlock("also")]);
    expect(assistantStore.getState().status).toBe("running");
    expect(assistantStore.getState().blocks).toMatchObject([
      { kind: "user", text: "first" },
      { kind: "user", text: "also", attachments: ["a.txt"] },
    ]);
    expect(assistantStore.getState().queuedPrompt).toBeNull();
    finishTurn();
    await sending;
    expect(conn.prompt).toHaveBeenCalledTimes(1);
  });

  it("steer promptRequired queues the message and sends it after the turn", async () => {
    let finishTurn!: () => void;
    const prompt = vi.fn(
      () =>
        new Promise<{ stopReason: "end_turn" }>((resolve) => {
          finishTurn = () => resolve({ stopReason: "end_turn" });
        }),
    );
    const { conn, connector } = fakeConnection({
      canSteer: true,
      prompt,
      steer: vi.fn(async () => "promptRequired" as const),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const sending = send("first");
    await assistantStore.steer([textBlock("then this")], {
      text: "then this",
      attachments: [],
    });
    expect(assistantStore.getState().queuedPrompt).toEqual([
      textBlock("then this"),
    ]);
    finishTurn();
    await vi.waitFor(() => expect(prompt).toHaveBeenCalledTimes(2));
    expect(prompt).toHaveBeenLastCalledWith("s-old", [textBlock("then this")]);
    expect(assistantStore.getState().status).toBe("running");
    finishTurn();
    await sending;
    expect(conn.prompt).toHaveBeenCalledTimes(2);
    expect(assistantStore.getState()).toMatchObject({
      status: "ready",
      queuedPrompt: null,
    });
    // Shown once, when steered; the fallback prompt adds no second block.
    expect(
      assistantStore.getState().blocks.filter((b) => b.kind === "user"),
    ).toHaveLength(2);
  });

  it("steer promptRequired after the turn already settled runs the prompt immediately", async () => {
    let finishTurn!: () => void;
    let answerSteer!: (outcome: "promptRequired") => void;
    const prompt = vi.fn(
      () =>
        new Promise<{ stopReason: "end_turn" }>((resolve) => {
          finishTurn = () => resolve({ stopReason: "end_turn" });
        }),
    );
    const { connector } = fakeConnection({
      canSteer: true,
      prompt,
      steer: vi.fn(
        () =>
          new Promise<"promptRequired">((resolve) => {
            answerSteer = resolve;
          }),
      ),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const sending = send("first");
    const steering = assistantStore.steer([textBlock("late")], {
      text: "late",
      attachments: [],
    });
    finishTurn();
    await sending;
    expect(assistantStore.getState().status).toBe("ready");
    answerSteer("promptRequired");
    await vi.waitFor(() => expect(prompt).toHaveBeenCalledTimes(2));
    expect(prompt).toHaveBeenLastCalledWith("s-old", [textBlock("late")]);
    expect(assistantStore.getState().status).toBe("running");
    finishTurn();
    await steering;
    expect(assistantStore.getState()).toMatchObject({
      status: "ready",
      queuedPrompt: null,
    });
    expect(
      assistantStore.getState().blocks.filter((b) => b.kind === "user"),
    ).toHaveLength(2);
  });

  it("stop drops a queued steering prompt", async () => {
    let finishTurn!: () => void;
    const prompt = vi.fn(
      () =>
        new Promise<{ stopReason: "end_turn" }>((resolve) => {
          finishTurn = () => resolve({ stopReason: "end_turn" });
        }),
    );
    const { connector } = fakeConnection({
      canSteer: true,
      prompt,
      steer: vi.fn(async () => "promptRequired" as const),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const sending = send("first");
    await assistantStore.steer([textBlock("x")], {
      text: "x",
      attachments: [],
    });
    assistantStore.stop();
    finishTurn();
    await sending;
    expect(prompt).toHaveBeenCalledTimes(1);
    expect(assistantStore.getState().status).toBe("ready");
  });

  it("stop cancels a steer that settles late with promptRequired", async () => {
    let finishTurn!: () => void;
    let answerSteer!: (outcome: "promptRequired") => void;
    const prompt = vi.fn(
      () =>
        new Promise<{ stopReason: "end_turn" }>((resolve) => {
          finishTurn = () => resolve({ stopReason: "end_turn" });
        }),
    );
    const { connector } = fakeConnection({
      canSteer: true,
      prompt,
      steer: vi.fn(
        () =>
          new Promise<"promptRequired">((resolve) => {
            answerSteer = resolve;
          }),
      ),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const sending = send("first");
    const steering = assistantStore.steer([textBlock("late")], {
      text: "late",
      attachments: [],
    });
    assistantStore.stop();
    finishTurn();
    await sending;
    answerSteer("promptRequired");
    await steering;
    expect(prompt).toHaveBeenCalledTimes(1);
    expect(assistantStore.getState()).toMatchObject({
      status: "ready",
      queuedPrompt: null,
    });
  });

  it("skips orphaned forks when choosing the newest chat to load", async () => {
    const { conn, connector } = fakeConnection({
      listSessions: vi.fn(async () => [
        { sessionId: "s-fork", cwd: "/home/alice", title: "Old chat (fork)" },
        { sessionId: "s-old", cwd: "/home/alice", title: "Old chat" },
      ]),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    expect(conn.loadSession).toHaveBeenCalledWith("s-old");
  });

  it("loads a fork when nothing else exists", async () => {
    const { conn, connector } = fakeConnection({
      listSessions: vi.fn(async () => [
        { sessionId: "s-fork", cwd: "/home/alice", title: "Old chat (fork)" },
      ]),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    expect(conn.loadSession).toHaveBeenCalledWith("s-fork");
  });

  it("setConfigOption keeps the old options while pending, then takes the response", async () => {
    let answer!: (options: ReturnType<typeof selectOption>[]) => void;
    const { conn, connector } = fakeConnection({
      listSessions: vi.fn(async () => []),
      newSession: vi.fn(async () => ({
        sessionId: "s-new",
        modes: null,
        configOptions: [selectOption("a")],
      })),
      setConfigOption: vi.fn(
        () =>
          new Promise<ReturnType<typeof selectOption>[]>((resolve) => {
            answer = resolve;
          }),
      ),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const changing = assistantStore.setConfigOption("model", "b");
    expect(conn.setConfigOption).toHaveBeenCalledWith("s-new", "model", "b");
    expect(assistantStore.getState().configOptions).toMatchObject([
      { currentValue: "a" },
    ]);
    answer([selectOption("b")]);
    await changing;
    expect(assistantStore.getState().configOptions).toMatchObject([
      { currentValue: "b" },
    ]);
  });

  it("setConfigOption response wins over a config_option_update still waiting for its frame", async () => {
    let answer!: (options: ReturnType<typeof selectOption>[]) => void;
    const { connector, handlers } = fakeConnection({
      listSessions: vi.fn(async () => []),
      newSession: vi.fn(async () => ({
        sessionId: "s-new",
        modes: null,
        configOptions: [selectOption("a")],
      })),
      setConfigOption: vi.fn(
        () =>
          new Promise<ReturnType<typeof selectOption>[]>((resolve) => {
            answer = resolve;
          }),
      ),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const changing = assistantStore.setConfigOption("model", "b");
    handlers().onUpdate({
      sessionId: "s-new",
      update: {
        sessionUpdate: "config_option_update",
        configOptions: [selectOption("a")],
      },
    });
    answer([selectOption("b")]);
    await changing;
    assistantStore._flush();
    expect(assistantStore.getState().configOptions).toMatchObject([
      { currentValue: "b" },
    ]);
  });

  it("setMode updates the current mode after the agent accepts it", async () => {
    const { conn, connector } = fakeConnection({
      listSessions: vi.fn(async () => []),
      newSession: vi.fn(async () => ({
        sessionId: "s-new",
        modes: {
          currentModeId: "default",
          availableModes: [
            { id: "default", name: "Default" },
            { id: "plan", name: "Plan" },
          ],
        },
        configOptions: [],
      })),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    await assistantStore.setMode("plan");
    expect(conn.setMode).toHaveBeenCalledWith("s-new", "plan");
    expect(assistantStore.getState().modes?.currentModeId).toBe("plan");
  });

  it("deleting the current session starts a new one", async () => {
    const listSessions = vi
      .fn()
      .mockResolvedValueOnce([{ sessionId: "s-old", cwd: "/home/alice" }])
      .mockResolvedValue([]);
    const { conn, connector } = fakeConnection({
      canDeleteSession: true,
      listSessions,
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    await assistantStore.deleteSession("s-old");
    expect(conn.deleteSession).toHaveBeenCalledWith("s-old");
    expect(conn.newSession).toHaveBeenCalled();
    expect(assistantStore.getState()).toMatchObject({
      sessionId: "s-new",
      sessions: [],
      status: "ready",
    });
  });

  it("deleting another session only drops it from the list", async () => {
    const { conn, connector } = fakeConnection({
      canDeleteSession: true,
      listSessions: vi.fn(async () => [
        { sessionId: "a", cwd: "/home/alice" },
        { sessionId: "b", cwd: "/home/alice" },
      ]),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    await assistantStore.deleteSession("b");
    expect(conn.newSession).not.toHaveBeenCalled();
    expect(assistantStore.getState()).toMatchObject({
      sessionId: "a",
      sessions: [{ sessionId: "a" }],
    });
  });

  it("steering twice while the agent cannot inject appends to the queue", async () => {
    let finishTurn!: () => void;
    const prompt = vi.fn(
      () =>
        new Promise<{ stopReason: "end_turn" }>((resolve) => {
          finishTurn = () => resolve({ stopReason: "end_turn" });
        }),
    );
    const { connector } = fakeConnection({
      canSteer: true,
      prompt,
      steer: vi.fn(async () => "promptRequired" as const),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const sending = send("first");
    for (const text of ["one", "two"]) {
      await assistantStore.steer([textBlock(text)], { text, attachments: [] });
    }
    finishTurn();
    await vi.waitFor(() => expect(prompt).toHaveBeenCalledTimes(2));
    expect(prompt).toHaveBeenLastCalledWith("s-old", [
      textBlock("one"),
      textBlock("two"),
    ]);
    finishTurn();
    await sending;
  });

  it("refuses to switch chats while a chat is loading", async () => {
    let finishLoad!: () => void;
    const { conn, connector } = fakeConnection({
      listSessions: vi.fn(async () => [
        { sessionId: "a", cwd: "/home/alice" },
        { sessionId: "b", cwd: "/home/alice" },
      ]),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    conn.loadSession = vi.fn(
      (sessionId: string) =>
        new Promise<SessionOpenResult>((resolve) => {
          finishLoad = () =>
            resolve({ sessionId, modes: null, configOptions: [] });
        }),
    );
    const loading = assistantStore.selectSession("b");
    expect(assistantStore.getState().status).toBe("connecting");
    await assistantStore.newSession();
    await assistantStore.selectSession("a");
    expect(conn.newSession).not.toHaveBeenCalled();
    expect(conn.loadSession).toHaveBeenCalledTimes(1);
    finishLoad();
    await loading;
    expect(assistantStore.getState()).toMatchObject({
      status: "ready",
      sessionId: "b",
    });
  });

  it("drops a late load for another chat but keeps updates for the new one", async () => {
    let finishLoad!: () => void;
    let finishNew!: () => void;
    const { conn, connector, handlers } = fakeConnection({
      listSessions: vi.fn(async () => [{ sessionId: "a", cwd: "/home/alice" }]),
    });
    assistantStore._setConnector(connector);
    await assistantStore.connect("claude");
    const chunk = (text: string) =>
      ({
        sessionUpdate: "agent_message_chunk",
        content: { type: "text", text },
      }) as const;
    conn.loadSession = vi.fn(
      (sessionId: string) =>
        new Promise<SessionOpenResult>((resolve) => {
          finishLoad = () => {
            handlers().onUpdate({ sessionId, update: chunk("A history") });
            resolve({
              sessionId,
              modes: { currentModeId: "m", availableModes: [] },
              configOptions: [selectOption("a")],
            });
          };
        }),
    );
    conn.newSession = vi.fn(
      () =>
        new Promise<SessionOpenResult>((resolve) => {
          finishNew = () => {
            handlers().onUpdate({
              sessionId: "s-new",
              update: {
                sessionUpdate: "available_commands_update",
                availableCommands: [{ name: "fresh", description: "" }],
              },
            });
            resolve({ sessionId: "s-new", modes: null, configOptions: [] });
          };
        }),
    );
    const loading = assistantStore.selectSession("a");
    // Simulate the state having moved on while A's load is still in flight.
    assistantStore._patch({ status: "ready" });
    const creating = assistantStore.newSession();
    finishNew();
    await creating;
    finishLoad();
    await loading;
    assistantStore._flush();
    expect(assistantStore.getState()).toMatchObject({
      status: "ready",
      sessionId: "s-new",
      blocks: [],
      modes: null,
      configOptions: [],
      commands: [{ name: "fresh" }],
    });
  });

  describe("side session", () => {
    const chunk = (text: string) =>
      ({
        sessionUpdate: "agent_message_chunk",
        content: { type: "text", text },
      }) as const;
    const connectWithFork = async (
      overrides: Partial<AssistantConnection> = {},
    ) => {
      const fake = fakeConnection({ canFork: true, ...overrides });
      assistantStore._setConnector(fake.connector);
      await assistantStore.connect("claude");
      return fake;
    };

    it("exposes canFork and turnStartedAt while a main turn runs", async () => {
      let finishTurn!: () => void;
      await connectWithFork({
        prompt: vi.fn(
          () =>
            new Promise<{ stopReason: "end_turn" }>((resolve) => {
              finishTurn = () => resolve({ stopReason: "end_turn" });
            }),
        ),
      });
      expect(assistantStore.getState()).toMatchObject({
        canFork: true,
        turnStartedAt: null,
        side: null,
      });
      const sending = send("hi");
      expect(assistantStore.getState().turnStartedAt).toEqual(
        expect.any(Number),
      );
      finishTurn();
      await sending;
      expect(assistantStore.getState().turnStartedAt).toBeNull();
    });

    it("forks the open chat once; a second openSide only re-opens", async () => {
      const { conn } = await connectWithFork();
      await assistantStore.openSide();
      expect(conn.forkSession).toHaveBeenCalledWith("s-old");
      expect(assistantStore.getState().side).toMatchObject({
        sessionId: "s-side",
        blocks: [],
        status: "ready",
        pending: null,
        open: true,
        error: null,
      });
      assistantStore.hideSide();
      expect(assistantStore.getState().side?.open).toBe(false);
      await assistantStore.openSide();
      expect(conn.forkSession).toHaveBeenCalledTimes(1);
      expect(assistantStore.getState().side?.open).toBe(true);
    });

    it("flags sideOpening while the fork runs, and clears it either way", async () => {
      let finishFork!: () => void;
      let failFork!: () => void;
      const forkSession = vi.fn(
        () =>
          new Promise<SessionOpenResult>((resolve, reject) => {
            finishFork = () =>
              resolve({ sessionId: "s-side", modes: null, configOptions: [] });
            failFork = () => reject(new Error("fork failed"));
          }),
      );
      await connectWithFork({ forkSession });
      const opening = assistantStore.openSide();
      expect(assistantStore.getState().sideOpening).toBe(true);
      finishFork();
      await opening;
      expect(assistantStore.getState()).toMatchObject({
        sideOpening: false,
        side: { sessionId: "s-side", open: true },
      });

      await assistantStore.discardSide();
      const failing = assistantStore.openSide();
      expect(assistantStore.getState().sideOpening).toBe(true);
      failFork();
      await failing;
      expect(assistantStore.getState()).toMatchObject({
        sideOpening: false,
        side: null,
        error: "fork failed",
      });
    });

    it("does not fork twice when openSide is called while the fork is in flight", async () => {
      let finishFork!: () => void;
      const { conn } = await connectWithFork({
        forkSession: vi.fn(
          () =>
            new Promise<SessionOpenResult>((resolve) => {
              finishFork = () =>
                resolve({
                  sessionId: "s-side",
                  modes: null,
                  configOptions: [],
                });
            }),
        ),
      });
      const first = assistantStore.openSide();
      const second = assistantStore.openSide();
      finishFork();
      await Promise.all([first, second]);
      expect(conn.forkSession).toHaveBeenCalledTimes(1);
    });

    it("does nothing when the agent cannot fork", async () => {
      const { conn } = await connectWithFork({ canFork: false });
      await assistantStore.openSide();
      expect(conn.forkSession).not.toHaveBeenCalled();
      expect(assistantStore.getState().side).toBeNull();
    });

    it("openSide(text) sends the text to the side session once the fork resolves", async () => {
      const { conn } = await connectWithFork();
      await assistantStore.openSide("what is this?");
      expect(conn.prompt).toHaveBeenCalledWith("s-side", [
        textBlock("what is this?"),
      ]);
      expect(assistantStore.getState().side).toMatchObject({
        sessionId: "s-side",
        open: true,
        blocks: [{ kind: "user" }],
      });
    });

    it("openSide(text) with an existing side sends immediately without forking again", async () => {
      const { conn } = await connectWithFork();
      await assistantStore.openSide();
      assistantStore.hideSide();
      await assistantStore.openSide("again");
      expect(conn.forkSession).toHaveBeenCalledTimes(1);
      expect(conn.prompt).toHaveBeenCalledWith("s-side", [textBlock("again")]);
      expect(assistantStore.getState().side?.open).toBe(true);
    });

    it("openSide(text) reports a busy side instead of dropping the question", async () => {
      const { conn } = await connectWithFork({
        prompt: vi.fn(() => new Promise<{ stopReason: "end_turn" }>(() => {})),
      });
      await assistantStore.openSide();
      void assistantStore.sendSide("first");
      await assistantStore.openSide("second");
      expect(conn.prompt).toHaveBeenCalledTimes(1);
      expect(assistantStore.getState().side?.error).toBe(
        "Side question is busy; try again when it finishes",
      );
    });

    it("openSide(text) during a fork in flight sends the queued text once it resolves", async () => {
      let finishFork!: () => void;
      const { conn } = await connectWithFork({
        forkSession: vi.fn(
          () =>
            new Promise<SessionOpenResult>((resolve) => {
              finishFork = () =>
                resolve({
                  sessionId: "s-side",
                  modes: null,
                  configOptions: [],
                });
            }),
        ),
      });
      const first = assistantStore.openSide();
      await assistantStore.openSide("queued");
      finishFork();
      await first;
      expect(conn.forkSession).toHaveBeenCalledTimes(1);
      expect(conn.prompt).toHaveBeenCalledWith("s-side", [textBlock("queued")]);
    });

    it("openSide(text) with a question already queued during a fork keeps the first and reports busy", async () => {
      let finishFork!: () => void;
      const { conn } = await connectWithFork({
        forkSession: vi.fn(
          () =>
            new Promise<SessionOpenResult>((resolve) => {
              finishFork = () =>
                resolve({
                  sessionId: "s-side",
                  modes: null,
                  configOptions: [],
                });
            }),
        ),
      });
      const first = assistantStore.openSide("one");
      await assistantStore.openSide("two");
      expect(assistantStore.getState().error).toBe(
        "Side question is busy; try again when it finishes",
      );
      finishFork();
      await first;
      expect(conn.prompt).toHaveBeenCalledTimes(1);
      expect(conn.prompt).toHaveBeenCalledWith("s-side", [textBlock("one")]);
    });

    it("routes interleaved updates to the transcript of their own session", async () => {
      const { handlers } = await connectWithFork();
      await assistantStore.openSide();
      handlers().onUpdate({ sessionId: "s-old", update: chunk("main-1 ") });
      handlers().onUpdate({ sessionId: "s-side", update: chunk("side-1 ") });
      handlers().onUpdate({ sessionId: "s-other", update: chunk("stray") });
      handlers().onUpdate({ sessionId: "s-old", update: chunk("main-2") });
      handlers().onUpdate({ sessionId: "s-side", update: chunk("side-2") });
      assistantStore._flush();
      const { blocks, side } = assistantStore.getState();
      expect(blocks).toMatchObject([{ kind: "agent", text: "main-1 main-2" }]);
      expect(side?.blocks).toMatchObject([
        { kind: "agent", text: "side-1 side-2" },
      ]);
    });

    it("sendSide prompts the side session and moves running to ready", async () => {
      let finishTurn!: () => void;
      const { conn, handlers } = await connectWithFork({
        prompt: vi.fn(
          () =>
            new Promise<{ stopReason: "end_turn" }>((resolve) => {
              finishTurn = () => resolve({ stopReason: "end_turn" });
            }),
        ),
      });
      await assistantStore.openSide();
      const sending = assistantStore.sendSide("why?");
      expect(assistantStore.getState().side).toMatchObject({
        status: "running",
        blocks: [{ kind: "user" }],
      });
      expect(assistantStore.getState().status).toBe("ready");
      handlers().onUpdate({ sessionId: "s-side", update: chunk("because") });
      finishTurn();
      await sending;
      expect(conn.prompt).toHaveBeenCalledWith("s-side", [textBlock("why?")]);
      expect(assistantStore.getState().side).toMatchObject({
        status: "ready",
        blocks: [{ kind: "user" }, { kind: "agent", text: "because" }],
      });
      expect(assistantStore.getState().blocks).toEqual([]);
    });

    it("records a side prompt failure on the side only", async () => {
      await connectWithFork({
        prompt: vi.fn(async () => {
          throw new Error("boom");
        }),
      });
      await assistantStore.openSide();
      await assistantStore.sendSide("x");
      expect(assistantStore.getState().side).toMatchObject({
        status: "ready",
        error: "boom",
      });
      expect(assistantStore.getState().error).toBeNull();
    });

    it("routes a side permission to side.pending and answers it", async () => {
      const { handlers } = await connectWithFork();
      await assistantStore.openSide();
      const answer = handlers().onPermission({
        sessionId: "s-side",
        toolCall: { toolCallId: "t", title: "side tool" },
        options: [{ optionId: "yes", name: "Allow", kind: "allow_once" }],
      });
      expect(assistantStore.getState().pending).toBeNull();
      expect(
        assistantStore.getState().side?.pending?.request.toolCall.title,
      ).toBe("side tool");
      assistantStore.answerSidePermission("yes");
      await expect(answer).resolves.toEqual({
        outcome: { outcome: "selected", optionId: "yes" },
      });
      expect(assistantStore.getState().side?.pending).toBeNull();
      const mainAnswer = handlers().onPermission({
        sessionId: "s-old",
        toolCall: { toolCallId: "m", title: "main tool" },
        options: [],
      });
      expect(assistantStore.getState().pending?.request.toolCall.title).toBe(
        "main tool",
      );
      expect(assistantStore.getState().side?.pending).toBeNull();
      assistantStore.answerPermission(null);
      await mainAnswer;
    });

    it("cancels permissions from a discarded side or an unknown session", async () => {
      const { handlers } = await connectWithFork();
      await assistantStore.openSide();
      await assistantStore.discardSide();
      const request = (sessionId: string) =>
        handlers().onPermission({
          sessionId,
          toolCall: { toolCallId: "t", title: "x" },
          options: [],
        });
      await expect(request("s-side")).resolves.toEqual({
        outcome: { outcome: "cancelled" },
      });
      await expect(request("s-unknown")).resolves.toEqual({
        outcome: { outcome: "cancelled" },
      });
      expect(assistantStore.getState().pending).toBeNull();
    });

    it("cancels a main permission that a newer main request replaces", async () => {
      const { handlers } = await connectWithFork();
      const request = (id: string) =>
        handlers().onPermission({
          sessionId: "s-old",
          toolCall: { toolCallId: id, title: id },
          options: [],
        });
      const first = request("one");
      void request("two");
      await expect(first).resolves.toEqual({
        outcome: { outcome: "cancelled" },
      });
      expect(assistantStore.getState().pending?.request.toolCall.title).toBe(
        "two",
      );
    });

    it("keeps main and side statuses independent, and stop cancels only main", async () => {
      let finishMain!: () => void;
      let finishSide!: () => void;
      const prompt = vi.fn(
        (sessionId: string) =>
          new Promise<{ stopReason: "end_turn" }>((resolve) => {
            if (sessionId === "s-side") {
              finishSide = () => resolve({ stopReason: "end_turn" });
            } else {
              finishMain = () => resolve({ stopReason: "end_turn" });
            }
          }),
      );
      const { conn } = await connectWithFork({ prompt });
      await assistantStore.openSide();
      const main = send("main");
      const side = assistantStore.sendSide("side");
      expect(assistantStore.getState().status).toBe("running");
      expect(assistantStore.getState().side?.status).toBe("running");
      assistantStore.stop();
      expect(conn.cancel).toHaveBeenCalledTimes(1);
      expect(conn.cancel).toHaveBeenCalledWith("s-old");
      finishSide();
      await side;
      expect(assistantStore.getState().status).toBe("running");
      expect(assistantStore.getState().side?.status).toBe("ready");
      finishMain();
      await main;
      expect(assistantStore.getState().status).toBe("ready");
    });

    it("keeps a side usage_update out of the main usage", async () => {
      const { handlers } = await connectWithFork();
      await assistantStore.openSide();
      handlers().onUpdate({
        sessionId: "s-side",
        update: { sessionUpdate: "usage_update", used: 5, size: 10 },
      });
      handlers().onUpdate({
        sessionId: "s-old",
        update: { sessionUpdate: "usage_update", used: 1, size: 10 },
      });
      assistantStore._flush();
      expect(assistantStore.getState().usage).toMatchObject({ used: 1 });
    });

    it("does not fork unless the chat is ready or running", async () => {
      const { conn } = await connectWithFork();
      assistantStore._patch({ status: "stopped" });
      await assistantStore.openSide();
      assistantStore._patch({ status: "connecting" });
      await assistantStore.openSide();
      expect(conn.forkSession).not.toHaveBeenCalled();
      expect(assistantStore.getState().side).toBeNull();
    });

    it("stopSide cancels the side session and its pending permission only", async () => {
      const { conn, handlers } = await connectWithFork();
      await assistantStore.openSide();
      const answer = handlers().onPermission({
        sessionId: "s-side",
        toolCall: { toolCallId: "t", title: "rm" },
        options: [],
      });
      assistantStore.stopSide();
      expect(conn.cancel).toHaveBeenCalledWith("s-side");
      expect(conn.cancel).not.toHaveBeenCalledWith("s-old");
      await expect(answer).resolves.toEqual({
        outcome: { outcome: "cancelled" },
      });
      expect(assistantStore.getState().side?.pending).toBeNull();
    });

    it("discardSide closes the session when the agent can and clears the side", async () => {
      const { conn } = await connectWithFork({ canClose: true });
      await assistantStore.openSide();
      await assistantStore.discardSide();
      expect(conn.closeSession).toHaveBeenCalledWith("s-side");
      expect(assistantStore.getState().side).toBeNull();
    });

    it("discardSide deletes the fork's transcript when the agent can", async () => {
      const { conn } = await connectWithFork({
        canClose: true,
        canDeleteSession: true,
      });
      await assistantStore.openSide();
      await assistantStore.discardSide();
      expect(conn.closeSession).toHaveBeenCalledWith("s-side");
      expect(conn.deleteSession).toHaveBeenCalledWith("s-side");
    });

    it("discardSide ignores a failing delete", async () => {
      const { conn } = await connectWithFork({
        canClose: true,
        canDeleteSession: true,
        deleteSession: vi.fn(async () => {
          throw new Error("nope");
        }),
      });
      await assistantStore.openSide();
      await expect(assistantStore.discardSide()).resolves.toBeUndefined();
      expect(conn.deleteSession).toHaveBeenCalledWith("s-side");
      expect(assistantStore.getState().side).toBeNull();
    });

    it("keeps the live side session out of the chat list", async () => {
      await connectWithFork();
      assistantStore._patch({
        sessions: [
          { sessionId: "s-side", cwd: "/home/alice" },
          { sessionId: "s-old", cwd: "/home/alice" },
        ],
      });
      await assistantStore.openSide();
      expect(
        assistantStore.getState().sessions.map((s) => s.sessionId),
      ).toEqual(["s-old"]);
    });

    it("discarding a running side cancels it when the agent cannot close", async () => {
      const { conn } = await connectWithFork({
        canClose: false,
        prompt: vi.fn(() => new Promise<never>(() => {})),
      });
      await assistantStore.openSide();
      void assistantStore.sendSide("slow");
      await assistantStore.discardSide();
      expect(conn.cancel).toHaveBeenCalledWith("s-side");
    });

    it("a side permission re-opens a hidden panel", async () => {
      const { handlers } = await connectWithFork();
      await assistantStore.openSide();
      assistantStore.hideSide();
      void handlers().onPermission({
        sessionId: "s-side",
        toolCall: { toolCallId: "t", title: "side tool" },
        options: [],
      });
      expect(assistantStore.getState().side?.open).toBe(true);
    });

    it("an agent exit while forking leaves no side behind", async () => {
      let finishFork!: () => void;
      const { handlers } = await connectWithFork({
        forkSession: vi.fn(
          () =>
            new Promise<SessionOpenResult>((resolve) => {
              finishFork = () =>
                resolve({
                  sessionId: "s-side",
                  modes: null,
                  configOptions: [],
                });
            }),
        ),
      });
      const opening = assistantStore.openSide();
      handlers().onExit(null);
      finishFork();
      await opening;
      expect(assistantStore.getState()).toMatchObject({
        status: "stopped",
        side: null,
      });
    });

    it("discardSide skips session/close when the agent cannot", async () => {
      const { conn } = await connectWithFork({ canClose: false });
      await assistantStore.openSide();
      await assistantStore.discardSide();
      expect(conn.closeSession).not.toHaveBeenCalled();
      expect(assistantStore.getState().side).toBeNull();
    });

    it("newSession discards the side", async () => {
      const { conn } = await connectWithFork({ canClose: true });
      await assistantStore.openSide();
      await assistantStore.newSession();
      expect(conn.closeSession).toHaveBeenCalledWith("s-side");
      expect(assistantStore.getState().side).toBeNull();
      expect(assistantStore.getState().sessionId).toBe("s-new");
    });

    it("selectSession and disconnect discard the side", async () => {
      const { conn } = await connectWithFork({
        canClose: true,
        listSessions: vi.fn(async () => [
          { sessionId: "a", cwd: "/home/alice" },
          { sessionId: "b", cwd: "/home/alice" },
        ]),
      });
      await assistantStore.openSide();
      await assistantStore.selectSession("b");
      expect(conn.closeSession).toHaveBeenCalledWith("s-side");
      expect(assistantStore.getState().side).toBeNull();
      await assistantStore.openSide();
      assistantStore.disconnect();
      expect(conn.closeSession).toHaveBeenCalledTimes(2);
      expect(assistantStore.getState().side).toBeNull();
    });

    it("deleting the open chat discards the side", async () => {
      const { conn } = await connectWithFork({
        canClose: true,
        canDeleteSession: true,
      });
      await assistantStore.openSide();
      await assistantStore.deleteSession("s-old");
      expect(conn.closeSession).toHaveBeenCalledWith("s-side");
      expect(assistantStore.getState().side).toBeNull();
    });

    it("drops a fork that finishes after the chat changed", async () => {
      let finishFork!: () => void;
      const { conn } = await connectWithFork({
        canClose: true,
        forkSession: vi.fn(
          () =>
            new Promise<SessionOpenResult>((resolve) => {
              finishFork = () =>
                resolve({
                  sessionId: "s-side",
                  modes: null,
                  configOptions: [],
                });
            }),
        ),
      });
      const opening = assistantStore.openSide();
      await assistantStore.newSession();
      finishFork();
      await opening;
      expect(assistantStore.getState().side).toBeNull();
      expect(conn.closeSession).toHaveBeenCalledWith("s-side");
    });
  });
});
