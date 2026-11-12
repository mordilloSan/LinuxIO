import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import { assistantStore } from "./assistant-store";
import AssistantPage from "./AssistantPage";

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  useStreamMux: () => ({ status: "open", isOpen: true, getStream: () => null }),
}));

vi.mock("./plugins/PluginsDialog", () => ({
  default: ({
    agent,
    open,
    onNewChat,
  }: {
    agent: string;
    open: boolean;
    onNewChat?: () => void;
  }) =>
    open ? (
      <div aria-label="Plugins" role="dialog">
        {agent} {onNewChat ? "can start a new chat" : "turn running"}
      </div>
    ) : null,
}));

beforeEach(() => {
  assistantStore.disconnect();
  vi.spyOn(assistantStore, "connect").mockImplementation(async () => {});
});

const setState = assistantStore._patch;

describe("AssistantPage plugins", () => {
  it("opens the plugin manager from /plugins", async () => {
    render(<AssistantPage agent="claude" />);
    setState({ status: "ready", sessionId: "s" });
    await userEvent.type(
      await screen.findByLabelText("Message"),
      "/plugins{Enter}",
    );
    expect(screen.getByRole("dialog", { name: "Plugins" })).toHaveTextContent(
      "claude can start a new chat",
    );
  });

  // A plugin that keeps the agent from starting must stay removable.
  it("opens the plugin manager from the alert when the agent has stopped", async () => {
    render(<AssistantPage agent="claude" />);
    setState({ status: "stopped", exit: { code: 1, stderr: "" } });
    await userEvent.click(
      await screen.findByRole("button", { name: "Manage plugins" }),
    );
    expect(screen.getByRole("dialog", { name: "Plugins" })).toBeInTheDocument();
  });

  it("opens the plugin manager from the alert when the agent failed to start", async () => {
    render(<AssistantPage agent="claude" />);
    setState({ status: "error", error: "spawn failed" });
    await userEvent.click(
      await screen.findByRole("button", { name: "Manage plugins" }),
    );
    expect(screen.getByRole("dialog", { name: "Plugins" })).toBeInTheDocument();
  });

  it("has no plugins button in the page header", async () => {
    render(<AssistantPage agent="claude" />);
    setState({ status: "ready", sessionId: "s" });
    await screen.findByLabelText("Message");
    expect(screen.queryByRole("button", { name: /plugins/i })).toBeNull();
  });

  it("offers no new chat from the plugin manager while a turn runs", async () => {
    render(<AssistantPage agent="claude" />);
    setState({ status: "running", sessionId: "s", canSteer: true });
    await userEvent.type(
      await screen.findByLabelText("Message"),
      "/plugins{Enter}",
    );
    expect(screen.getByRole("dialog", { name: "Plugins" })).toHaveTextContent(
      "turn running",
    );
  });

  it("has no plugin manager for an agent without a plugin CLI", async () => {
    render(<AssistantPage agent="gemini" />);
    setState({ status: "ready", sessionId: "s" });
    await userEvent.type(await screen.findByLabelText("Message"), "/");
    await screen.findByRole("menuitem", { name: "/btw" });
    expect(
      screen.queryByRole("menuitem", { name: "/plugins" }),
    ).not.toBeInTheDocument();
    setState({ status: "stopped", exit: { code: 1, stderr: "" } });
    await screen.findByRole("button", { name: "Restart" });
    expect(
      screen.queryByRole("button", { name: "Manage plugins" }),
    ).not.toBeInTheDocument();
  });
});
