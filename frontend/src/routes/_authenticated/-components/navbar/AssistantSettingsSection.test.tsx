import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ConfigContext } from "@/contexts/ConfigContext";
import { createConfigContextValue, render } from "@/test/render";

import AssistantSettingsSection from "./AssistantSettingsSection";

const mocks = vi.hoisted(() => ({
  agent: "" as string,
  probe: vi.fn(),
  logout: vi.fn(),
  authenticate: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@/hooks/useConfig", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useConfig")>()),
  useAssistantSettings: () => ({ agent: mocks.agent }),
}));
vi.mock("@/api/acp/probe", () => ({
  probeAssistant: mocks.probe,
  logoutAssistant: mocks.logout,
  authenticateAssistant: mocks.authenticate,
}));
vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => mocks.navigate,
}));

beforeEach(() => {
  mocks.agent = "";
  mocks.probe.mockReset();
  mocks.logout.mockReset();
  mocks.authenticate.mockReset();
  mocks.navigate.mockReset();
});

describe("AssistantSettingsSection", () => {
  it("saves the chosen agent", async () => {
    const updateConfig = vi.fn();
    render(
      <ConfigContext.Provider
        value={createConfigContextValue({ updateConfig })}
      >
        <AssistantSettingsSection />
      </ConfigContext.Provider>,
      { capabilities: { nodeAvailable: true } },
    );
    // AppSelect is a combobox + listbox, not a native <select>.
    await userEvent.click(screen.getByLabelText("Agent"));
    await userEvent.click(screen.getByRole("option", { name: "Gemini CLI" }));
    expect(updateConfig).toHaveBeenCalledWith({
      assistant: { agent: "gemini" },
    });
  });

  it("shows login status and a login button from the probe", async () => {
    mocks.agent = "claude";
    mocks.probe.mockResolvedValue({
      status: "auth_required",
      authStatus: null,
      authMethods: [
        {
          id: "claude-ai-login",
          name: "Claude Subscription",
          type: "terminal",
          args: ["--cli", "auth", "login", "--claudeai"],
        },
      ],
      canLogout: false,
    });
    render(<AssistantSettingsSection />, {
      capabilities: { nodeAvailable: true },
    });
    expect(await screen.findByText(/not logged in/i)).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Claude Subscription" }),
    );
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/terminal",
      state: {
        terminalLogin: {
          agent: "claude",
          args: ["--cli", "auth", "login", "--claudeai"],
        },
      },
    });
  });

  it("sends authenticate for an agent-handled method and re-probes", async () => {
    mocks.agent = "codex";
    mocks.probe.mockResolvedValue({
      status: "auth_required",
      authStatus: null,
      authMethods: [{ id: "api-key", name: "API key" }],
      canLogout: false,
    });
    mocks.authenticate.mockResolvedValue(undefined);
    render(<AssistantSettingsSection />, {
      capabilities: { nodeAvailable: true },
    });
    await userEvent.click(
      await screen.findByRole("button", { name: "API key" }),
    );
    await waitFor(() =>
      expect(mocks.authenticate).toHaveBeenCalledWith("codex", "api-key"),
    );
    await waitFor(() => expect(mocks.probe).toHaveBeenCalledTimes(2));
  });

  it("shows the account and a log out button when logged in", async () => {
    mocks.agent = "claude";
    mocks.probe.mockResolvedValue({
      status: "logged_in",
      authStatus: {
        kind: "account",
        label: "Claude Max",
        account: { email: "a@example.com" },
      },
      authMethods: [],
      canLogout: true,
    });
    mocks.logout.mockResolvedValue(undefined);
    render(<AssistantSettingsSection />, {
      capabilities: { nodeAvailable: true },
    });
    expect(await screen.findByText(/Claude Max/)).toBeInTheDocument();
    expect(screen.getByText(/a@example.com/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /log out/i }));
    await waitFor(() => expect(mocks.logout).toHaveBeenCalledWith("claude"));
  });
});
