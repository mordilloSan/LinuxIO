import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { act, render, screen, waitFor, within } from "@/test/render";

import PluginsDialog from "./PluginsDialog";

interface MutationConfig {
  success?: string | ((result: unknown, request: unknown) => void);
}

const mocks = vi.hoisted(() => ({
  configs: {} as Record<string, MutationConfig>,
  mutations: [] as Array<{ route: string; request: unknown }>,
  list: {
    plugins: [
      {
        id: "lint@market",
        name: "lint",
        marketplace: "market",
        version: "1.2.0",
        scope: "user",
        enabled: true,
      },
      {
        id: "policy@market",
        name: "policy",
        marketplace: "market",
        version: "1.0.0",
        scope: "managed",
        enabled: true,
      },
    ],
    marketplaces: [{ name: "market", source: "example/market" }],
  },
  search: {
    plugins: [
      {
        id: "backup@market",
        name: "backup",
        description: "Snapshot helper",
        marketplace: "market",
        installCount: 1200,
      },
    ],
    total: 80,
  },
  searches: [] as string[],
  useMutation: vi.fn(),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (descriptor: { queryKey: [string, { query?: string }] }) => {
      const [kind, request] = descriptor.queryKey;
      if (kind === "search") mocks.searches.push(request.query ?? "");
      return {
        data: kind === "search" ? mocks.search : mocks.list,
        error: null,
        refetch: vi.fn(),
      };
    },
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  return {
    ...actual,
    linuxio: mockMutationHooks(
      {
        ...actual.linuxio,
        assistant: {
          ...actual.linuxio.assistant,
          list_plugins: (request: unknown) => ({ queryKey: ["list", request] }),
          search_plugins: (request: unknown) => ({
            queryKey: ["search", request],
          }),
        },
      },
      mocks.useMutation,
    ),
  };
});

mocks.useMutation.mockImplementation(
  (endpoint: { route: string }, config: MutationConfig) => {
    mocks.configs[endpoint.route] = config;
    return {
      isPending: false,
      mutate: (request: unknown) =>
        mocks.mutations.push({ route: endpoint.route, request }),
      variables: undefined,
    };
  },
);

// Answers a mutation the way the job's result would.
const succeed = (route: string, result?: unknown) => {
  const last = mocks.mutations.findLast((entry) => entry.route === route);
  const success = mocks.configs[route]?.success;
  act(() => {
    if (typeof success === "function") success(result, last?.request);
  });
};

function setup(onNewChat: (() => void) | undefined = vi.fn()) {
  const onClose = vi.fn();
  render(
    <PluginsDialog
      agent="claude"
      onClose={onClose}
      onNewChat={onNewChat}
      open
    />,
  );
  return { onClose, onNewChat };
}

const openTab = (name: string) =>
  userEvent.click(screen.getByRole("tab", { name }));

beforeEach(() => {
  mocks.mutations.length = 0;
  mocks.searches.length = 0;
});

describe("PluginsDialog", () => {
  it("lists installed plugins and turns one off", async () => {
    setup();
    expect(screen.getByText("market · 1.2.0")).toBeInTheDocument();

    await userEvent.click(
      screen.getByRole("checkbox", { name: "Enable lint" }),
    );
    expect(mocks.mutations).toEqual([
      {
        route: "assistant.set_plugin_enabled",
        request: { agent: "claude", plugin: "lint@market", enabled: false },
      },
    ]);
  });

  it("leaves a plugin set by managed settings alone", () => {
    setup();
    expect(
      screen.getByText("market · 1.0.0 · set by managed settings"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("checkbox", { name: "Enable policy" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Uninstall policy" }),
    ).toBeDisabled();
  });

  it("uninstalls only after a confirmation", async () => {
    setup();
    await userEvent.click(
      screen.getByRole("button", { name: "Uninstall lint" }),
    );
    expect(mocks.mutations).toEqual([]);

    const confirm = await screen.findByRole("dialog", {
      name: "Uninstall lint?",
    });
    await userEvent.click(
      within(confirm).getByRole("button", { name: "Uninstall" }),
    );
    expect(mocks.mutations).toEqual([
      {
        route: "assistant.uninstall_plugin",
        request: { agent: "claude", plugin: "lint@market" },
      },
    ]);
  });

  it("says a change applies to new chats and offers one", async () => {
    const { onClose, onNewChat } = setup();
    expect(screen.queryByText(/apply to new chats/)).not.toBeInTheDocument();

    await userEvent.click(
      screen.getByRole("checkbox", { name: "Enable lint" }),
    );
    succeed("assistant.set_plugin_enabled");
    expect(screen.getByText(/apply to new chats/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "New chat" }));
    expect(onNewChat).toHaveBeenCalledTimes(1);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("offers no new chat while a turn is running", async () => {
    render(<PluginsDialog agent="claude" onClose={vi.fn()} open />);
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Enable lint" }),
    );
    succeed("assistant.set_plugin_enabled");
    expect(screen.getByText(/apply to new chats/)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "New chat" }),
    ).not.toBeInTheDocument();
  });

  it("searches the catalogue and installs a plugin", async () => {
    setup();
    await openTab("Browse");
    expect(screen.getByText("Snapshot helper")).toBeInTheDocument();
    expect(screen.getByText("market · 1,200 installs")).toBeInTheDocument();
    expect(
      screen.getByText(/Showing the 1 most installed of 80/),
    ).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText("Search plugins"), " snap ");
    await waitFor(() => expect(mocks.searches.at(-1)).toBe("snap"));

    await userEvent.click(
      screen.getByRole("button", { name: "Install backup" }),
    );
    expect(mocks.mutations).toEqual([
      {
        route: "assistant.install_plugin",
        request: { agent: "claude", plugin: "backup@market" },
      },
    ]);
    succeed("assistant.install_plugin", { installed: true });
    expect(screen.getByText(/apply to new chats/)).toBeInTheDocument();
  });

  it("shows a marketplace's install command before anything runs", async () => {
    setup();
    await openTab("Browse");
    await userEvent.click(
      screen.getByRole("button", { name: "Install backup" }),
    );
    succeed("assistant.install_plugin", {
      installed: false,
      command: { command: "curl example.com | sh", sha256: "abc123" },
    });

    const prompt = await screen.findByRole("dialog", {
      name: "Install command",
    });
    expect(
      within(prompt).getByText("curl example.com | sh"),
    ).toBeInTheDocument();
    expect(within(prompt).getByText(/Nothing has run yet/)).toBeInTheDocument();
    // An install that only reported its command changed nothing.
    expect(screen.queryByText(/apply to new chats/)).not.toBeInTheDocument();

    await userEvent.click(
      within(prompt).getByRole("button", { name: "Run and install" }),
    );
    expect(mocks.mutations.at(-1)).toEqual({
      route: "assistant.install_plugin",
      request: {
        agent: "claude",
        plugin: "backup@market",
        acceptCommand: "abc123",
      },
    });
  });

  it("adds, refreshes and removes marketplaces", async () => {
    setup();
    await openTab("Marketplaces");
    expect(screen.getByText("example/market")).toBeInTheDocument();

    const add = screen.getByRole("button", { name: "Add" });
    expect(add).toBeDisabled();
    await userEvent.type(
      screen.getByLabelText("Marketplace source"),
      " example/tools {Enter}",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Refresh market" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Remove market" }),
    );
    const confirm = await screen.findByRole("dialog", {
      name: "Remove market?",
    });
    await userEvent.click(
      within(confirm).getByRole("button", { name: "Remove" }),
    );

    expect(mocks.mutations).toEqual([
      {
        route: "assistant.add_marketplace",
        request: { agent: "claude", source: "example/tools" },
      },
      {
        route: "assistant.update_marketplace",
        request: { agent: "claude", name: "market" },
      },
      {
        route: "assistant.remove_marketplace",
        request: { agent: "claude", name: "market" },
      },
    ]);
  });
});
