import { QueryClientProvider } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";

import type { AppConfig, UIConfig } from "@/api";
import { AuthContext } from "@/contexts/AuthContext";

const apiMocks = vi.hoisted(() => ({
  configGetCall: vi.fn(),
  uiGetCall: vi.fn(),
  configSetUseAction: vi.fn(),
  uiSetUseAction: vi.fn(),
  dockerListComposeProjectsQueryKey: [
    "linuxio",
    "docker",
    "list_compose_projects",
  ],
  setConfigRemote: vi.fn(),
  setUIRemote: vi.fn(),
  useStreamMux: vi.fn(),
}));

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: toastMocks,
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    call: apiMocks.configGetCall,
    useCallMutation: (endpoint: { route?: string }, config: unknown) => {
      if (endpoint.route === "config.set") {
        apiMocks.configSetUseAction(config);
      } else if (endpoint.route === "config.set_ui") {
        apiMocks.uiSetUseAction(config);
      }
      return {
        mutate:
          endpoint.route === "config.set_ui"
            ? apiMocks.setUIRemote
            : apiMocks.setConfigRemote,
        mutateAsync:
          endpoint.route === "config.set_ui"
            ? apiMocks.setUIRemote
            : apiMocks.setConfigRemote,
      };
    },
    useStreamMux: apiMocks.useStreamMux,
    linuxio: {
      ...actual.linuxio,
      config: {
        get: {
          queryKey: ["linuxio", "config", "get"],
          queryFn: () =>
            apiMocks.configGetCall("config.get", undefined, {
              staleTime: 0,
            }),
        },
        get_ui: {
          queryKey: ["linuxio", "config", "get_ui"],
          queryFn: () =>
            apiMocks.uiGetCall("config.get_ui", undefined, {
              staleTime: 0,
            }),
        },
        set: {
          route: "config.set",
        },
        set_ui: {
          route: "config.set_ui",
        },
      },
      docker: {
        ...actual.linuxio.docker,
        list_compose_projects: {
          queryKey: apiMocks.dockerListComposeProjectsQueryKey,
        },
      },
    },
  };
});
const { LinuxIOError } = await import("@/api");
const { ConfigProvider } = await import("@/contexts/ConfigProvider");
const { useConfig, useConfigValue, useDockerSettings } =
  await import("@/hooks/useConfig");
const { act, createAuthContextValue, createTestQueryClient, render } =
  await import("@/test/render");

function remoteConfig(overrides: Partial<AppConfig> = {}): AppConfig {
  return {
    appSettings: {
      chunkSizeMB: 1,
      showHiddenFiles: true,
    },
    docker: {
      folders: ["/srv/docker"],
      requireMountsForFolders: false,
      proxy: {
        baseDomain: "linuxio.test",
        caddyEnabled: true,
        tlsEmail: "ops@linuxio.test",
      },
    },
    jobs: {
      archiveCompressionWorkers: 2,
      archiveExtractWorkers: 2,
      heavyArchiveConcurrency: 1,
      notificationMinIntervalMs: 800,
      progressMinBytesMB: 8,
      progressMinIntervalMs: 200,
    },
    storageMode: "home",
    ...overrides,
  };
}

type TestUIConfig = UIConfig & { viewModeDefault: "card" | "table" };

function remoteUI(overrides: Partial<TestUIConfig> = {}): TestUIConfig {
  return {
    primaryColor: "#123456",
    theme: "LIGHT",
    sidebarCollapsed: false,
    navigationMode: "sidebar",
    dockTileColors: "accent",
    dockAccentGradient: {
      startColor: "",
      endColor: "",
      rangeStart: 0,
      rangeEnd: 100,
    },
    hiddenCards: [],
    dockerDashboardSections: {
      overview: true,
      monitoring: true,
      daemon: true,
      resources: true,
    },
    hardwareSections: {
      overview: true,
      hardware: true,
      sensors: true,
      systemInfo: true,
      gpu: true,
      pciDevices: true,
      memoryModules: true,
    },
    layoutOrders: { dashboard: ["overview"] },
    viewModes: {
      "docker.images": "table",
    },
    viewModeDefault: "card",
    terminalFontSize: 16,
    ...overrides,
  };
}

const onSavedSpy = vi.fn();

function Probe() {
  const { isLoaded, setKey, updateConfig } = useConfig();
  const [theme] = useConfigValue("theme");
  const [themeColors] = useConfigValue("themeColors");
  const [showHiddenFiles] = useConfigValue("showHiddenFiles");
  const [chunkSizeMB] = useConfigValue("chunkSizeMB");
  const [dockAccentGradient] = useConfigValue("dockAccentGradient");
  const [dockerDashboardSections] = useConfigValue("dockerDashboardSections");
  const [viewModes] = useConfigValue("viewModes");
  const docker = useDockerSettings();
  return (
    <div>
      <div data-testid="loaded">{String(isLoaded)}</div>
      <div data-testid="theme">{theme}</div>
      <div data-testid="theme-colors">{JSON.stringify(themeColors)}</div>
      <div data-testid="show-hidden-files">{String(showHiddenFiles)}</div>
      <div data-testid="chunk-size">{String(chunkSizeMB)}</div>
      <div data-testid="dock-accent-gradient">
        {JSON.stringify(dockAccentGradient)}
      </div>
      <div data-testid="docker-folders">{docker.folders.join(",")}</div>
      <div data-testid="docker-dashboard-sections">
        {JSON.stringify(dockerDashboardSections)}
      </div>
      <div data-testid="view-modes">{JSON.stringify(viewModes)}</div>
      <button onClick={() => setKey("theme", "DARK")}>set theme</button>
      <button onClick={() => setKey("primaryColor", "#abcdef")}>
        set primary color
      </button>
      <button onClick={() => setKey("themeColors", undefined)}>
        reset colors
      </button>
      <button onClick={() => setKey("showHiddenFiles", false)}>
        set hidden files
      </button>
      <button onClick={() => setKey("chunkSizeMB", 4)}>set chunk four</button>
      <button onClick={() => setKey("chunkSizeMB", 8)}>set chunk eight</button>
      <button
        onClick={() =>
          updateConfig({
            docker: {
              folders: ["/opt/compose"],
            },
          })
        }
      >
        set folders
      </button>
      <button
        onClick={() =>
          updateConfig(
            { docker: { requireMountsForFolders: true } },
            onSavedSpy,
          )
        }
      >
        set mounts
      </button>
      <button
        onClick={() =>
          updateConfig(
            {
              appSettings: { showHiddenFiles: false, theme: "DARK" },
            },
            onSavedSpy,
          )
        }
      >
        set mixed settings
      </button>
    </div>
  );
}

interface CapturedActionConfig {
  invalidates?:
    | readonly (readonly unknown[])[]
    | ((
        result: unknown,
        variables: unknown,
      ) => readonly (readonly unknown[])[]);
  success?: string | ((result: unknown, variables: unknown) => void);
}

function renderProvider({
  configQueryFn = async () => remoteConfig(),
  uiQueryFn = async () => remoteUI(),
  sessionExpired = vi.fn(),
  strictMode = false,
  queryClient: providedQueryClient,
}: {
  configQueryFn?: () => Promise<AppConfig>;
  uiQueryFn?: () => Promise<UIConfig>;
  sessionExpired?: () => void;
  strictMode?: boolean;
  queryClient?: ReturnType<typeof createTestQueryClient>;
} = {}) {
  const actionConfigs: CapturedActionConfig[] = [];
  const queryClient = providedQueryClient ?? createTestQueryClient();
  const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");

  apiMocks.useStreamMux.mockReturnValue({
    status: "open",
    isOpen: true,
    getStream: () => null,
  });
  apiMocks.configGetCall.mockImplementation(
    (_route: string, _request: undefined, _options: unknown) => configQueryFn(),
  );
  apiMocks.uiGetCall.mockImplementation(
    (_route: string, _request: undefined, _options: unknown) => uiQueryFn(),
  );
  apiMocks.configSetUseAction.mockImplementation((config) => {
    actionConfigs.push(config);
    return { mutate: apiMocks.setConfigRemote };
  });
  apiMocks.uiSetUseAction.mockImplementation((config) => {
    actionConfigs.push(config);
    return { mutate: apiMocks.setUIRemote };
  });

  // Emulates useAction's success path: invalidates -> success, with the
  // bounded action result.
  const fireActionSuccess = (result: unknown, variables: unknown) => {
    for (const config of actionConfigs) {
      const keys =
        typeof config.invalidates === "function"
          ? config.invalidates(result, variables)
          : config.invalidates;
      for (const queryKey of keys ?? []) {
        void queryClient.invalidateQueries({ queryKey });
      }
      if (typeof config.success === "function") {
        config.success(result, variables);
      } else if (config.success !== undefined) {
        toastMocks.success(config.success);
      }
    }
  };

  const provider = (
    <ConfigProvider>
      <Probe />
    </ConfigProvider>
  );

  const rendered = render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider
        value={createAuthContextValue({
          isAuthenticated: true,
          sessionExpired,
          user: { id: "miguel", name: "Miguel" },
        })}
      >
        {strictMode ? <StrictMode>{provider}</StrictMode> : provider}
      </AuthContext.Provider>
    </QueryClientProvider>,
    // The provider under test owns its cache: no pre-seeded snapshots.
    { seedConfig: false },
  );

  return {
    fireActionSuccess,
    invalidateQueries,
    actionConfigs,
    queryClient,
    sessionExpired,
    unmount: rendered.unmount,
  };
}

describe("ConfigProvider", () => {
  it("loads both authoritative bridge files without a client cache", async () => {
    renderProvider();

    expect(await screen.findByTestId("loaded")).toHaveTextContent("true");
    expect(screen.getByTestId("theme")).toHaveTextContent("LIGHT");
    expect(screen.getByTestId("docker-folders")).toHaveTextContent(
      "/srv/docker",
    );
    expect(screen.getByTestId("dock-accent-gradient")).toHaveTextContent(
      '{"startColor":"","endColor":"","rangeStart":0,"rangeEnd":100}',
    );

    expect(apiMocks.configGetCall).toHaveBeenCalledWith(
      "config.get",
      undefined,
      { staleTime: 0 },
    );
    expect(apiMocks.uiGetCall).toHaveBeenCalledWith(
      "config.get_ui",
      undefined,
      {
        staleTime: 0,
      },
    );
    expect(sessionStorage.length).toBe(0);
  });

  it("clears scoped config snapshots when the authenticated provider ends", async () => {
    const first = renderProvider();
    await screen.findByTestId("loaded");
    expect(
      first.queryClient.getQueryData([
        "linuxio",
        "config",
        "get",
        "user",
        "miguel",
      ]),
    ).toBeDefined();
    expect(
      first.queryClient.getQueryData([
        "linuxio",
        "config",
        "get_ui",
        "user",
        "miguel",
      ]),
    ).toBeDefined();

    first.unmount();

    expect(
      first.queryClient.getQueryData([
        "linuxio",
        "config",
        "get",
        "user",
        "miguel",
      ]),
    ).toBeUndefined();
    expect(
      first.queryClient.getQueryData([
        "linuxio",
        "config",
        "get_ui",
        "user",
        "miguel",
      ]),
    ).toBeUndefined();

    apiMocks.uiGetCall.mockClear();
    renderProvider({ queryClient: first.queryClient });
    await screen.findByTestId("loaded");
    expect(apiMocks.uiGetCall).toHaveBeenCalledTimes(1);
  });

  it("saves user changes only after a successful backend load", async () => {
    const { actionConfigs } = renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set theme" }).click();
    });

    expect(apiMocks.setUIRemote).toHaveBeenCalledTimes(1);
    expect(apiMocks.setUIRemote.mock.calls[0]).toHaveLength(1);
    const expectedSnapshot: Partial<TestUIConfig> = {
      ...remoteUI({ theme: "DARK" }),
    };
    delete expectedSnapshot.viewModeDefault;
    expect(apiMocks.setUIRemote.mock.calls[0]?.[0]).toEqual(expectedSnapshot);
    expect(actionConfigs.length).toBeGreaterThan(0);
    expect(sessionStorage.length).toBe(0);
  });

  it("sends important settings to the bridge config endpoint", async () => {
    renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set hidden files" }).click();
    });

    expect(apiMocks.setConfigRemote).toHaveBeenCalledWith({
      appSettings: { showHiddenFiles: false },
    });
    expect(apiMocks.setUIRemote).not.toHaveBeenCalled();
  });

  it("attempts mixed bridge and UI writes independently", async () => {
    onSavedSpy.mockClear();
    apiMocks.setConfigRemote.mockRejectedValueOnce(new Error("bridge failed"));
    apiMocks.setUIRemote.mockResolvedValueOnce(undefined);
    renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set mixed settings" }).click();
    });

    await waitFor(() => expect(apiMocks.setConfigRemote).toHaveBeenCalled());
    await waitFor(() => expect(apiMocks.setUIRemote).toHaveBeenCalled());
    expect(onSavedSpy).not.toHaveBeenCalled();
    expect(apiMocks.setConfigRemote).toHaveBeenCalledWith({
      appSettings: { showHiddenFiles: false },
    });
    expect(apiMocks.setUIRemote).toHaveBeenCalledWith(
      expect.objectContaining({ theme: "DARK" }),
    );
  });

  it("persists a StrictMode-replayed update only once", async () => {
    renderProvider({ strictMode: true });

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set theme" }).click();
    });

    expect(apiMocks.setUIRemote).toHaveBeenCalledTimes(1);
    expect(apiMocks.setUIRemote.mock.calls[0]).toHaveLength(1);
    expect(apiMocks.setUIRemote.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({ theme: "DARK", primaryColor: "#123456" }),
    );
  });

  it("serializes UI replacement writes in browser event order", async () => {
    let resolveFirst: (() => void) | undefined;
    apiMocks.setUIRemote
      .mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            resolveFirst = resolve;
          }),
      )
      .mockResolvedValue(undefined);
    renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set theme" }).click();
      screen.getByRole("button", { name: "set primary color" }).click();
    });
    await waitFor(() => expect(apiMocks.setUIRemote).toHaveBeenCalledTimes(1));
    resolveFirst?.();
    await waitFor(() => expect(apiMocks.setUIRemote).toHaveBeenCalledTimes(2));
    expect(apiMocks.setUIRemote.mock.calls[0]?.[0]).not.toHaveProperty(
      "primaryColor",
      "#abcdef",
    );
    expect(apiMocks.setUIRemote.mock.calls[1]?.[0]).toEqual(
      expect.objectContaining({ primaryColor: "#abcdef" }),
    );
  });

  it("drops queued saves when the authenticated config provider unmounts", async () => {
    let resolveFirst: (() => void) | undefined;
    apiMocks.setUIRemote
      .mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            resolveFirst = resolve;
          }),
      )
      .mockResolvedValue(undefined);
    const { unmount } = renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set theme" }).click();
      screen.getByRole("button", { name: "set primary color" }).click();
    });
    await waitFor(() => expect(apiMocks.setUIRemote).toHaveBeenCalledTimes(1));

    unmount();
    await act(async () => resolveFirst?.());
    expect(apiMocks.setUIRemote).toHaveBeenCalledTimes(1);
  });

  it("serializes important-setting writes in browser event order", async () => {
    let resolveFirst: (() => void) | undefined;
    apiMocks.setConfigRemote
      .mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            resolveFirst = resolve;
          }),
      )
      .mockResolvedValue(undefined);
    renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set chunk four" }).click();
      screen.getByRole("button", { name: "set chunk eight" }).click();
    });

    await waitFor(() =>
      expect(apiMocks.setConfigRemote).toHaveBeenCalledTimes(1),
    );
    resolveFirst?.();
    await waitFor(() =>
      expect(apiMocks.setConfigRemote).toHaveBeenCalledTimes(2),
    );
    expect(apiMocks.setConfigRemote.mock.calls[0]?.[0]).toEqual({
      appSettings: { chunkSizeMB: 4 },
    });
    expect(apiMocks.setConfigRemote.mock.calls[1]?.[0]).toEqual({
      appSettings: { chunkSizeMB: 8 },
    });
  });

  it("resets UI fields by omitting them from the replacement payload", async () => {
    renderProvider({
      uiQueryFn: async () =>
        remoteUI({ themeColors: { dark: { codeText: "red" } } }),
    });

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "reset colors" }).click();
    });

    const [payload] = apiMocks.setUIRemote.mock.lastCall ?? [];
    expect(payload).not.toHaveProperty("themeColors");
  });

  it("prunes stored view modes that match the backend default", async () => {
    renderProvider({
      uiQueryFn: async () =>
        remoteUI({
          viewModeDefault: "table",
          viewModes: {
            "docker.images": "table",
            "docker.networks": "card",
          },
        }),
    });

    await screen.findByTestId("loaded");
    expect(screen.getByTestId("view-modes")).toHaveTextContent(
      '{"docker.networks":"card"}',
    );
  });

  it("invalidates compose projects after persisted Docker folder changes", async () => {
    const { fireActionSuccess, invalidateQueries } = renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set folders" }).click();
    });
    fireActionSuccess(undefined, {
      docker: {
        folders: ["/opt/compose"],
      },
    });

    expect(apiMocks.setConfigRemote).toHaveBeenCalledWith({
      docker: {
        folders: ["/opt/compose"],
      },
    });
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ["linuxio", "docker", "list_compose_projects"],
    });
  });

  it("invokes onSaved only after the backend confirms the save", async () => {
    let resolveSave: (() => void) | undefined;
    apiMocks.setConfigRemote.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          resolveSave = resolve;
        }),
    );
    renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set mounts" }).click();
    });

    expect(onSavedSpy).not.toHaveBeenCalled();
    const [patch] = apiMocks.setConfigRemote.mock.lastCall ?? [];
    expect(patch).toEqual({ docker: { requireMountsForFolders: true } });
    resolveSave?.();
    await waitFor(() => expect(onSavedSpy).toHaveBeenCalledTimes(1));
  });

  it("signs out and does not render children on auth failures", async () => {
    const sessionExpired = vi.fn();

    renderProvider({
      configQueryFn: async () => {
        throw new LinuxIOError("expired", 401);
      },
      sessionExpired,
    });

    // The notice + redirect are sessionExpired's contract (covered in the
    // AuthContext suite); here we only assert ConfigProvider delegates to it.
    await waitFor(() => expect(sessionExpired).toHaveBeenCalledTimes(1));
    expect(screen.queryByTestId("loaded")).not.toBeInTheDocument();
  });

  it("does not render until both backend snapshots load", async () => {
    renderProvider({
      configQueryFn: async () => {
        throw new LinuxIOError("unavailable", 503);
      },
      uiQueryFn: async () => {
        throw new LinuxIOError("unavailable", 503);
      },
    });

    await waitFor(() => expect(screen.queryByTestId("theme")).toBeNull());
  });
});
