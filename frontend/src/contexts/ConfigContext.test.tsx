import { QueryClientProvider } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";

import type { AppConfig } from "@/api";
import { AuthContext } from "@/contexts/AuthContext";
import { writeConfigCache } from "@/utils/configCache";

const apiMocks = vi.hoisted(() => ({
  configGetCall: vi.fn(),
  configSetUseAction: vi.fn(),
  dockerListComposeProjectsQueryKey: [
    "linuxio",
    "docker",
    "list_compose_projects",
  ],
  setConfigRemote: vi.fn(),
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
      }
      return { mutate: apiMocks.setConfigRemote };
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
        set: {
          route: "config.set",
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
const { useConfig } = await import("@/hooks/useConfig");
const { act, createAuthContextValue, createTestQueryClient, render } =
  await import("@/test/render");

function remoteConfig(overrides: Partial<AppConfig> = {}): AppConfig {
  return {
    appSettings: {
      chunkSizeMB: 1,
      hiddenCards: [],
      layoutOrders: { dashboard: ["overview"] },
      primaryColor: "#123456",
      showHiddenFiles: true,
      sidebarCollapsed: false,
      theme: "LIGHT",
      viewModes: {
        "docker.images": "table",
      },
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
    ...overrides,
  };
}

const onSavedSpy = vi.fn();

function Probe() {
  const { config, isLoaded, setKey, updateConfig } = useConfig();
  return (
    <div>
      <div data-testid="loaded">{String(isLoaded)}</div>
      <div data-testid="theme">{config.appSettings.theme}</div>
      <div data-testid="dock-accent-gradient">
        {JSON.stringify(config.appSettings.dockAccentGradient)}
      </div>
      <div data-testid="docker-folders">{config.docker.folders.join(",")}</div>
      <div data-testid="docker-dashboard-sections">
        {JSON.stringify(config.appSettings.dockerDashboardSections)}
      </div>
      <button onClick={() => setKey("theme", "DARK")}>set theme</button>
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
  sessionExpired = vi.fn(),
  strictMode = false,
}: {
  configQueryFn?: () => Promise<AppConfig>;
  sessionExpired?: () => void;
  strictMode?: boolean;
} = {}) {
  const actionConfigs: CapturedActionConfig[] = [];
  const queryClient = createTestQueryClient();
  const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");

  apiMocks.useStreamMux.mockReturnValue({
    status: "open",
    isOpen: true,
    getStream: () => null,
  });
  apiMocks.configGetCall.mockImplementation(
    (_route: string, _request: undefined, _options: unknown) => configQueryFn(),
  );
  apiMocks.configSetUseAction.mockImplementation((config) => {
    actionConfigs.push(config);
    return { mutate: apiMocks.setConfigRemote };
  });

  // Emulates useAction's success path: invalidates -> success, with the
  // bounded action result.
  const fireActionSuccess = (result: unknown, variables: unknown) => {
    const config = actionConfigs.at(-1);
    if (!config) return;
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
  };

  const provider = (
    <ConfigProvider>
      <Probe />
    </ConfigProvider>
  );

  render(
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
  );

  return {
    fireActionSuccess,
    invalidateQueries,
    actionConfigs,
    queryClient,
    sessionExpired,
  };
}

describe("ConfigProvider", () => {
  it("loads backend config after the stream mux is ready and caches defaults", async () => {
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
    expect(sessionStorage.getItem("linuxio_config:miguel")).toContain(
      "/srv/docker",
    );
  });

  it("uses cached config without refetching when a user cache exists", async () => {
    const cached = remoteConfig();
    writeConfigCache("miguel", {
      ...cached,
      docker: { ...cached.docker, folders: ["/cached"] },
    });

    renderProvider();

    expect(await screen.findByTestId("docker-folders")).toHaveTextContent(
      "/cached",
    );
    expect(apiMocks.configGetCall).not.toHaveBeenCalled();
  });

  it("fills missing cached Docker dashboard section defaults", async () => {
    const cached = remoteConfig({
      appSettings: {
        ...remoteConfig().appSettings,
        dockerDashboardSections: {
          overview: false,
          daemon: false,
          resources: true,
        } as AppConfig["appSettings"]["dockerDashboardSections"],
      },
    });
    writeConfigCache("miguel", cached);

    renderProvider();

    expect(
      await screen.findByTestId("docker-dashboard-sections"),
    ).toHaveTextContent(
      '{"overview":false,"monitoring":true,"daemon":false,"resources":true}',
    );
    expect(apiMocks.configGetCall).not.toHaveBeenCalled();
  });

  it("preserves an explicit cached Docker monitoring section value", async () => {
    const cached = remoteConfig({
      appSettings: {
        ...remoteConfig().appSettings,
        dockerDashboardSections: {
          overview: true,
          monitoring: false,
          daemon: true,
          resources: false,
        },
      },
    });
    writeConfigCache("miguel", cached);

    renderProvider();

    expect(
      await screen.findByTestId("docker-dashboard-sections"),
    ).toHaveTextContent(
      '{"overview":true,"monitoring":false,"daemon":true,"resources":false}',
    );
  });

  it("fills missing Docker dashboard section defaults from the backend config", async () => {
    const backend = remoteConfig({
      appSettings: {
        ...remoteConfig().appSettings,
        dockerDashboardSections: {
          overview: false,
          daemon: true,
          resources: false,
        } as AppConfig["appSettings"]["dockerDashboardSections"],
      },
    });

    renderProvider({ configQueryFn: async () => backend });

    expect(
      await screen.findByTestId("docker-dashboard-sections"),
    ).toHaveTextContent(
      '{"overview":false,"monitoring":true,"daemon":true,"resources":false}',
    );
  });

  it("saves user changes only after a successful backend load", async () => {
    const { actionConfigs } = renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set theme" }).click();
    });

    expect(apiMocks.setConfigRemote).toHaveBeenCalledWith({
      appSettings: {
        theme: "DARK",
      },
    });
    expect(actionConfigs.length).toBeGreaterThan(0);
    expect(sessionStorage.getItem("linuxio_config:miguel")).toContain(
      '"theme":"DARK"',
    );
  });

  it("persists a StrictMode-replayed update only once", async () => {
    renderProvider({ strictMode: true });

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set theme" }).click();
    });

    expect(apiMocks.setConfigRemote).toHaveBeenCalledTimes(1);
    expect(apiMocks.setConfigRemote).toHaveBeenCalledWith({
      appSettings: {
        theme: "DARK",
      },
    });
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
    renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set mounts" }).click();
    });

    // The per-save callback is forwarded as the mutate call's onSuccess and
    // must not fire before the mutation succeeds.
    expect(onSavedSpy).not.toHaveBeenCalled();
    const [patch, mutateOptions] = apiMocks.setConfigRemote.mock.lastCall ?? [];
    expect(patch).toEqual({ docker: { requireMountsForFolders: true } });
    (mutateOptions as { onSuccess: () => void }).onSuccess();
    expect(onSavedSpy).toHaveBeenCalledTimes(1);
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

  it("falls back to defaults without saving when the stream API is unavailable", async () => {
    const consoleWarn = vi
      .spyOn(console, "warn")
      .mockImplementation(() => undefined);

    renderProvider({
      configQueryFn: async () => {
        throw new LinuxIOError("unavailable", 503);
      },
    });

    expect(await screen.findByTestId("theme")).toHaveTextContent("DARK");
    await act(async () => {
      screen.getByRole("button", { name: "set theme" }).click();
    });

    expect(apiMocks.setConfigRemote).not.toHaveBeenCalled();
    expect(sessionStorage.getItem("linuxio_config:miguel")).toBeNull();
    expect(consoleWarn).toHaveBeenCalledWith(
      "Stream API unavailable, using default config",
    );
  });
});
