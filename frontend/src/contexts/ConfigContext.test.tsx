import { QueryClientProvider } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { AppConfig } from "@/api";
import { AuthContext } from "@/contexts/AuthContext";
import { writeConfigCache } from "@/utils/configCache";

const apiMocks = vi.hoisted(() => ({
  configGetFetch: vi.fn(),
  configSetUseJobAction: vi.fn(),
  dockerListComposeProjectsQueryKey: vi.fn(() => [
    "linuxio",
    "docker",
    "list_compose_projects",
  ]),
  setConfigRemote: vi.fn(),
  waitForStreamMux: vi.fn(),
}));

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: toastMocks,
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    waitForStreamMux: apiMocks.waitForStreamMux,
    linuxio: {
      ...actual.linuxio,
      config: {
        get: {
          useFetcher: () => apiMocks.configGetFetch,
        },
        set: {
          useJobAction: apiMocks.configSetUseJobAction,
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
const { ConfigProvider } = await import("@/contexts/ConfigContext");
const { useConfig } = await import("@/hooks/useConfig");
const { act, createAuthContextValue, createTestQueryClient, render } =
  await import("@/test/render");

function remoteConfig(overrides: Partial<AppConfig> = {}): AppConfig {
  return {
    appSettings: {
      chunkSizeMB: 1,
      containerOrder: [],
      dashboardOrder: ["overview"],
      hiddenCards: [],
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

function Probe() {
  const { config, isLoaded, setKey, updateConfig } = useConfig();
  return (
    <div>
      <div data-testid="loaded">{String(isLoaded)}</div>
      <div data-testid="theme">{config.appSettings.theme}</div>
      <div data-testid="docker-folders">{config.docker.folders.join(",")}</div>
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
    </div>
  );
}

interface CapturedJobActionConfig {
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
  signOut = vi.fn(),
}: {
  configQueryFn?: () => Promise<AppConfig>;
  signOut?: () => Promise<void>;
} = {}) {
  const jobActionConfigs: CapturedJobActionConfig[] = [];
  const queryClient = createTestQueryClient();
  const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");

  apiMocks.waitForStreamMux.mockResolvedValue(true);
  apiMocks.configGetFetch.mockImplementation(() => configQueryFn());
  apiMocks.configSetUseJobAction.mockImplementation((config) => {
    jobActionConfigs.push(config);
    return { mutate: apiMocks.setConfigRemote };
  });

  // Emulates useJobAction's success path: invalidates -> success, with the
  // unwrapped job result (not a JobSnapshot).
  const fireJobActionSuccess = (result: unknown, variables: unknown) => {
    const config = jobActionConfigs.at(-1);
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

  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider
        value={createAuthContextValue({
          isAuthenticated: true,
          signOut,
          user: { id: "miguel", name: "Miguel" },
        })}
      >
        <ConfigProvider>
          <Probe />
        </ConfigProvider>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );

  return {
    fireJobActionSuccess,
    invalidateQueries,
    jobActionConfigs,
    queryClient,
    signOut,
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

    expect(apiMocks.waitForStreamMux).toHaveBeenCalledWith(250);
    expect(apiMocks.configGetFetch).toHaveBeenCalledWith({
      staleTime: 0,
    });
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
    expect(apiMocks.configGetFetch).not.toHaveBeenCalled();
  });

  it("saves user changes only after a successful backend load", async () => {
    const { jobActionConfigs } = renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set theme" }).click();
    });

    expect(apiMocks.setConfigRemote).toHaveBeenCalledWith({
      appSettings: {
        theme: "DARK",
      },
    });
    expect(jobActionConfigs.length).toBeGreaterThan(0);
    expect(sessionStorage.getItem("linuxio_config:miguel")).toContain(
      '"theme":"DARK"',
    );
  });

  it("invalidates compose projects after persisted Docker folder changes", async () => {
    const { fireJobActionSuccess, invalidateQueries } = renderProvider();

    await screen.findByTestId("loaded");
    await act(async () => {
      screen.getByRole("button", { name: "set folders" }).click();
    });
    fireJobActionSuccess(undefined, {
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

  it("shows a toast after Docker mount ordering changes are persisted", async () => {
    const { fireJobActionSuccess } = renderProvider();

    await screen.findByTestId("loaded");

    fireJobActionSuccess(undefined, {
      docker: {
        requireMountsForFolders: true,
      },
    });
    expect(toastMocks.success).toHaveBeenCalledWith(
      "Docker will wait for configured folder mounts.",
    );

    fireJobActionSuccess(undefined, {
      docker: {
        requireMountsForFolders: false,
      },
    });
    expect(toastMocks.success).toHaveBeenCalledWith(
      "Docker folder mount ordering disabled.",
    );
  });

  it("signs out and does not render children on auth failures", async () => {
    const signOut = vi.fn(async () => undefined);

    renderProvider({
      configQueryFn: async () => {
        throw new LinuxIOError("expired", 401);
      },
      signOut,
    });

    await waitFor(() => expect(signOut).toHaveBeenCalledTimes(1));

    expect(toastMocks.error).toHaveBeenCalledWith(
      "Session expired. Please sign in again.",
    );
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
