import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  HeaderActionSlotProvider,
  useHeaderActionSlot,
} from "@/contexts/HeaderActionSlotContext";
import { render } from "@/test/render";

import { assistantStore } from "./assistant-store";
import AssistantPage from "./AssistantPage";

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  useStreamMux: () => ({ status: "open", isOpen: true, getStream: () => null }),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => mocks.navigate,
}));

const mocks = vi.hoisted(() => ({ navigate: vi.fn() }));
const connectSpy = vi.fn(async () => {});

beforeEach(() => {
  mocks.navigate.mockReset();
  assistantStore.disconnect();
  connectSpy.mockClear();
  vi.spyOn(assistantStore, "connect").mockImplementation(connectSpy);
});

const setState = assistantStore._patch;

describe("AssistantPage", () => {
  it("asks to pick an agent when none is configured", () => {
    render(<AssistantPage agent={null} />);
    expect(
      screen.getByText(/choose an agent in settings/i),
    ).toBeInTheDocument();
    expect(connectSpy).not.toHaveBeenCalled();
  });

  it("connects to the configured agent on mount", () => {
    render(<AssistantPage agent="claude" />);
    expect(connectSpy).toHaveBeenCalledWith("claude");
  });

  it("lists previous chats in a dropdown from the history icon and loads one", async () => {
    const selectSession = vi
      .spyOn(assistantStore, "selectSession")
      .mockResolvedValue();
    render(<AssistantPage agent="claude" />);
    setState({
      status: "ready",
      sessionId: "s2",
      canListSessions: true,
      canLoadSession: true,
      sessions: [
        { sessionId: "s2", cwd: "/h", title: "Working session" },
        { sessionId: "s1", cwd: "/h", title: "Initial greeting" },
      ],
    });
    await userEvent.click(
      await screen.findByRole("button", { name: "Previous chats" }),
    );
    const menu = await screen.findByRole("menu", { name: "Previous chats" });
    expect(menu).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: "Working session" }),
    ).toHaveClass("app-menu__item--selected");
    await userEvent.click(
      screen.getByRole("menuitem", { name: "Initial greeting" }),
    );
    expect(selectSession).toHaveBeenCalledWith("s1");
  });

  describe("deleting a previous chat", () => {
    const sessions = [
      { sessionId: "s2", cwd: "/h", title: "Working session" },
      { sessionId: "s1", cwd: "/h", title: "Initial greeting" },
    ];

    it("confirms before deleting and does not select the chat", async () => {
      const deleteSession = vi
        .spyOn(assistantStore, "deleteSession")
        .mockResolvedValue();
      const selectSession = vi
        .spyOn(assistantStore, "selectSession")
        .mockResolvedValue();
      render(<AssistantPage agent="claude" />);
      setState({
        status: "ready",
        sessionId: "s2",
        canListSessions: true,
        canLoadSession: true,
        canDeleteSession: true,
        sessions,
      });
      await userEvent.click(
        await screen.findByRole("button", { name: "Previous chats" }),
      );
      await userEvent.click(
        await screen.findByRole("button", {
          name: "Delete Initial greeting",
        }),
      );
      expect(selectSession).not.toHaveBeenCalled();
      expect(deleteSession).not.toHaveBeenCalled();
      expect(screen.queryByRole("menu", { name: "Previous chats" })).toBeNull();
      await userEvent.click(
        await screen.findByRole("button", { name: "Delete" }),
      );
      expect(deleteSession).toHaveBeenCalledWith("s1");
    });

    it("does nothing when the confirmation is cancelled", async () => {
      const deleteSession = vi
        .spyOn(assistantStore, "deleteSession")
        .mockResolvedValue();
      render(<AssistantPage agent="claude" />);
      setState({
        status: "ready",
        sessionId: "s2",
        canListSessions: true,
        canDeleteSession: true,
        sessions,
      });
      await userEvent.click(
        await screen.findByRole("button", { name: "Previous chats" }),
      );
      await userEvent.click(
        await screen.findByRole("button", { name: "Delete Working session" }),
      );
      await userEvent.click(
        await screen.findByRole("button", { name: "Cancel" }),
      );
      expect(deleteSession).not.toHaveBeenCalled();
    });

    it("has no delete buttons when the agent cannot delete", async () => {
      render(<AssistantPage agent="claude" />);
      setState({
        status: "ready",
        sessionId: "s2",
        canListSessions: true,
        sessions,
      });
      await userEvent.click(
        await screen.findByRole("button", { name: "Previous chats" }),
      );
      await screen.findByRole("menu", { name: "Previous chats" });
      expect(screen.queryByRole("button", { name: /^Delete / })).toBeNull();
    });

    it("only blocks deleting the open chat while a turn is running", async () => {
      render(<AssistantPage agent="claude" />);
      setState({
        status: "running",
        sessionId: "s2",
        canListSessions: true,
        canDeleteSession: true,
        sessions,
      });
      await userEvent.click(
        await screen.findByRole("button", { name: "Previous chats" }),
      );
      expect(
        await screen.findByRole("button", { name: "Delete Working session" }),
      ).toBeDisabled();
      expect(
        screen.getByRole("button", { name: "Delete Initial greeting" }),
      ).toBeEnabled();
    });
  });

  it("renders the history and new chat icons in the header slot when one exists", async () => {
    function Harness() {
      const slot = useHeaderActionSlot();
      return (
        <>
          <div data-testid="header-host" ref={slot?.mount} />
          <AssistantPage agent="claude" />
        </>
      );
    }
    render(
      <HeaderActionSlotProvider>
        <Harness />
      </HeaderActionSlotProvider>,
    );
    setState({ status: "ready", sessionId: "s", canListSessions: true });
    const host = screen.getByTestId("header-host");
    expect(
      await within(host).findByRole("button", { name: "Previous chats" }),
    ).toBeInTheDocument();
    expect(
      within(host).getByRole("button", { name: "New chat" }),
    ).toBeInTheDocument();
    expect(document.querySelector(".assistant__actions")).toBeNull();
  });

  it("shows the context gauge instead of a usage caption", async () => {
    render(<AssistantPage agent="claude" />);
    setState({ status: "ready", sessionId: "s" });
    expect(screen.queryByRole("img", { name: /tokens/ })).toBeNull();
    setState({
      usage: {
        used: 12345,
        size: 200000,
        cost: { amount: 0.42, currency: "USD" },
      },
    });
    const gauge = await screen.findByRole("img", {
      name: /12\.3k \/ 200k tokens/,
    });
    expect(gauge.getAttribute("aria-label")).toMatch(/0\.42/);
    expect(screen.queryByText(/tokens/)).toBeNull();
  });

  it("disables chat switching while a chat is loading", async () => {
    render(<AssistantPage agent="claude" />);
    setState({
      status: "connecting",
      sessionId: "s",
      canListSessions: true,
      canLoadSession: true,
      sessions: [{ sessionId: "s", cwd: "/h", title: "Working session" }],
    });
    expect(
      await screen.findByRole("button", { name: "New chat" }),
    ).toBeDisabled();
    await userEvent.click(
      screen.getByRole("button", { name: "Previous chats" }),
    );
    expect(
      await screen.findByRole("menuitem", { name: "Working session" }),
    ).toBeDisabled();
  });

  it("starts a new chat from the header icon", async () => {
    const newSession = vi
      .spyOn(assistantStore, "newSession")
      .mockResolvedValue();
    render(<AssistantPage agent="claude" />);
    setState({ status: "ready", sessionId: "s" });
    await userEvent.click(
      await screen.findByRole("button", { name: "New chat" }),
    );
    expect(newSession).toHaveBeenCalled();
  });

  it("renders transcript blocks, a permission card, and answers it", async () => {
    const resolve = vi.fn();
    render(<AssistantPage agent="claude" />);
    setState({
      status: "running",
      blocks: [
        { kind: "user", id: "1", text: "list files", attachments: [] },
        { kind: "agent", id: "2", text: "Sure, running `ls`." },
        {
          kind: "tool",
          id: "3",
          toolCallId: "t1",
          title: "ls -la",
          toolKind: "execute",
          status: "pending",
          content: [],
        },
      ],
      pending: {
        request: {
          sessionId: "s",
          toolCall: { toolCallId: "t1", title: "ls -la" },
          options: [{ optionId: "ok", name: "Allow once", kind: "allow_once" }],
        },
        resolve,
      },
    });
    expect(await screen.findByText("list files")).toBeInTheDocument();
    expect(
      screen.getByText("ls -la", { selector: ".assistant-tool__title" }),
    ).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Allow once" }));
    expect(resolve).toHaveBeenCalledWith({
      outcome: { outcome: "selected", optionId: "ok" },
    });
  });

  it("shows the exit tail and a restart button when the agent stops", async () => {
    const restart = vi.spyOn(assistantStore, "restart").mockResolvedValue();
    render(<AssistantPage agent="claude" />);
    setState({
      status: "stopped",
      exit: { code: 127, stderr: "npx: command not found" },
    });
    expect(await screen.findByText(/agent stopped/i)).toBeInTheDocument();
    expect(screen.getByText(/npx: command not found/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /restart/i }));
    expect(restart).toHaveBeenCalled();
  });

  it("offers login when the agent needs authentication", async () => {
    render(<AssistantPage agent="claude" />);
    setState({
      status: "auth_required",
      authMethods: [
        {
          id: "claude-login",
          name: "Log in with Claude",
          type: "terminal",
          args: ["--cli"],
        },
      ],
    });
    await userEvent.click(
      await screen.findByRole("button", { name: "Log in with Claude" }),
    );
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/terminal",
      state: { terminalLogin: { agent: "claude", args: ["--cli"] } },
    });
  });

  it("offers restart when the connection fails", async () => {
    const restart = vi.spyOn(assistantStore, "restart").mockResolvedValue();
    render(<AssistantPage agent="claude" />);
    setState({ status: "error", error: "boom" });
    await userEvent.click(
      await screen.findByRole("button", { name: /restart/i }),
    );
    expect(restart).toHaveBeenCalled();
  });

  it("names the agent by its label while starting", async () => {
    render(<AssistantPage agent="claude" />);
    setState({ status: "connecting" });
    expect(await screen.findByText(/Starting Claude Code/)).toBeInTheDocument();
  });

  it("sends the composer text and disables it while running", async () => {
    const send = vi.spyOn(assistantStore, "send").mockResolvedValue();
    render(<AssistantPage agent="claude" />);
    setState({ status: "ready", sessionId: "s" });
    const input = await screen.findByLabelText("Message");
    await userEvent.type(input, "hello{Enter}");
    expect(send).toHaveBeenCalledWith([{ type: "text", text: "hello" }], {
      text: "hello",
      attachments: [],
    });
  });

  it("shows a prompt error while the session is still ready", async () => {
    render(<AssistantPage agent="claude" />);
    setState({ status: "ready", sessionId: "s", error: "boom" });
    expect(await screen.findByText("boom")).toBeInTheDocument();
  });

  it("stops a running turn on Escape", async () => {
    const stop = vi.spyOn(assistantStore, "stop").mockImplementation(() => {});
    render(<AssistantPage agent="claude" />);
    setState({ status: "running", sessionId: "s" });
    (await screen.findByRole("button", { name: "Stop" })).focus();
    await userEvent.keyboard("{Escape}");
    expect(stop).toHaveBeenCalledTimes(1);
  });

  it("stops a running turn on Escape even when focus is on the body", async () => {
    const stop = vi.spyOn(assistantStore, "stop").mockImplementation(() => {});
    render(<AssistantPage agent="claude" />);
    setState({ status: "running", sessionId: "s", canSteer: false });
    const input = await screen.findByLabelText("Message");
    expect(input).not.toBeDisabled();
    (document.activeElement as HTMLElement | null)?.blur();
    expect(document.body).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    expect(stop).toHaveBeenCalledTimes(1);
  });

  it("does not stop the turn when Escape closes a dialog", async () => {
    const stop = vi.spyOn(assistantStore, "stop").mockImplementation(() => {});
    render(<AssistantPage agent="claude" />);
    setState({
      status: "running",
      sessionId: "s2",
      canListSessions: true,
      canDeleteSession: true,
      sessions: [
        { sessionId: "s2", cwd: "/h", title: "Working session" },
        { sessionId: "s1", cwd: "/h", title: "Initial greeting" },
      ],
    });
    await userEvent.click(
      await screen.findByRole("button", { name: "Previous chats" }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Delete Initial greeting" }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.keyDown(within(dialog).getByRole("button", { name: "Delete" }), {
      key: "Escape",
    });
    expect(stop).not.toHaveBeenCalled();
  });

  it("starts a new chat with Ctrl+Shift+O", async () => {
    const newSession = vi
      .spyOn(assistantStore, "newSession")
      .mockResolvedValue();
    render(<AssistantPage agent="claude" />);
    setState({ status: "ready", sessionId: "s" });
    const input = await screen.findByLabelText("Message");
    await userEvent.click(input);
    await userEvent.keyboard("{Control>}{Shift>}o{/Shift}{/Control}");
    expect(newSession).toHaveBeenCalledTimes(1);
  });

  it("shows the status line only while a turn runs", async () => {
    render(<AssistantPage agent="claude" />);
    setState({ status: "ready", sessionId: "s" });
    await screen.findByLabelText("Message");
    expect(screen.queryByText(/Thinking…/)).not.toBeInTheDocument();
    setState({ status: "running", turnStartedAt: Date.now() });
    expect(await screen.findByText(/Thinking…/)).toBeInTheDocument();
    setState({ status: "ready", turnStartedAt: null });
    await waitFor(() =>
      expect(screen.queryByText(/Thinking…/)).not.toBeInTheDocument(),
    );
  });

  it("renders the config controls inside the composer bar", async () => {
    const setConfigOption = vi
      .spyOn(assistantStore, "setConfigOption")
      .mockResolvedValue();
    render(<AssistantPage agent="claude" />);
    setState({
      status: "ready",
      sessionId: "s",
      configOptions: [
        {
          id: "model",
          name: "Model",
          type: "select",
          category: "model",
          currentValue: "opus",
          options: [
            { value: "opus", name: "Opus" },
            { value: "sonnet", name: "Sonnet" },
          ],
        },
      ],
    });
    const model = await screen.findByRole("button", { name: "Model: Opus" });
    expect(model.closest(".assistant__composer-bar")).not.toBeNull();
    expect(document.querySelector(".assistant__controls")).toBeNull();
    await userEvent.click(model);
    await userEvent.click(
      await screen.findByRole("menuitem", { name: /Sonnet/ }),
    );
    expect(setConfigOption).toHaveBeenCalledWith("model", "sonnet");
  });

  describe("side question", () => {
    const side = {
      sessionId: "side1",
      blocks: [{ kind: "agent" as const, id: "sa", text: "A side answer" }],
      status: "ready" as const,
      pending: null,
      open: true,
      error: null,
    };

    it("has no header side-question icon", async () => {
      render(<AssistantPage agent="claude" />);
      setState({ status: "ready", sessionId: "s", canFork: true });
      await screen.findByRole("button", { name: "New chat" });
      expect(
        screen.queryByRole("button", { name: "Side question" }),
      ).toBeNull();
    });

    it("opens a side question from /btw in the composer", async () => {
      const openSide = vi.spyOn(assistantStore, "openSide").mockResolvedValue();
      render(<AssistantPage agent="claude" />);
      setState({ status: "ready", sessionId: "s", canFork: true });
      await userEvent.type(
        await screen.findByLabelText("Message"),
        "/btw why?{Enter}",
      );
      expect(openSide).toHaveBeenCalledWith("why?");
    });

    it("says so when /btw is used and the agent cannot fork", async () => {
      const openSide = vi.spyOn(assistantStore, "openSide").mockResolvedValue();
      render(<AssistantPage agent="claude" />);
      setState({ status: "ready", sessionId: "s", canFork: false });
      await userEvent.type(
        await screen.findByLabelText("Message"),
        "/btw why?{Enter}",
      );
      expect(
        await screen.findByText("This agent cannot open side questions"),
      ).toBeInTheDocument();
      expect(openSide).not.toHaveBeenCalled();
    });

    it("shows an opening panel while the fork runs", async () => {
      render(<AssistantPage agent="claude" />);
      setState({ status: "ready", sessionId: "s", canFork: true });
      await screen.findByLabelText("Message");
      setState({ sideOpening: true });
      const panel = await screen.findByRole("complementary", {
        name: "Side question",
      });
      expect(panel).toHaveAttribute("aria-busy", "true");
      expect(panel).toHaveTextContent(/Opening a side question/);
      setState({ sideOpening: false, side });
      expect(await screen.findByText("A side answer")).toBeInTheDocument();
      expect(
        screen.getByRole("complementary", { name: "Side question" }),
      ).not.toHaveAttribute("aria-busy");
    });

    it("shows the panel only while side.open", async () => {
      render(<AssistantPage agent="claude" />);
      setState({ status: "ready", sessionId: "s", canFork: true });
      await screen.findByLabelText("Message");
      expect(screen.queryByLabelText("Side question message")).toBeNull();
      setState({ side });
      expect(await screen.findByText("A side answer")).toBeInTheDocument();
      expect(
        screen.getByLabelText("Side question message"),
      ).toBeInTheDocument();
      setState({ side: { ...side, open: false } });
      await waitFor(() =>
        expect(screen.queryByLabelText("Side question message")).toBeNull(),
      );
    });

    it("Escape in the side field never stops the main turn", async () => {
      const stop = vi
        .spyOn(assistantStore, "stop")
        .mockImplementation(() => {});
      const stopSide = vi
        .spyOn(assistantStore, "stopSide")
        .mockImplementation(() => {});
      render(<AssistantPage agent="claude" />);
      setState({ status: "running", sessionId: "s", canFork: true, side });
      const field = await screen.findByLabelText("Side question message");
      await userEvent.click(field);
      await userEvent.keyboard("{Escape}");
      expect(stop).not.toHaveBeenCalled();
      expect(stopSide).not.toHaveBeenCalled();
      setState({ side: { ...side, status: "running" } });
      await userEvent.click(field);
      await userEvent.keyboard("{Escape}");
      expect(stop).not.toHaveBeenCalled();
      expect(stopSide).toHaveBeenCalledTimes(1);
    });
  });
});
