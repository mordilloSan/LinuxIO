import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import {
  render as rtlRender,
  type RenderOptions,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  createContext,
  type ReactElement,
  type ReactNode,
  useContext,
} from "react";

import type {
  AppConfig,
  ConfigContextType,
  EffectiveAppSettings,
  UIConfig,
} from "@/api";
import {
  capabilitiesQueryKey,
  wireFromCapabilityState,
} from "@/api/capabilities";
import type { CapabilityState } from "@/api/capabilities";
import { bridgeConfigQueryKey, uiConfigQueryKey } from "@/api/config-query";
import { ANONYMOUS_USER_ID, cacheUserId } from "@/api/user-scope";
import { AuthContext, type AuthContextType } from "@/contexts/AuthContext";
import { ConfigContext } from "@/contexts/ConfigContext";
import { createBackgroundTaskCache } from "@/tasks/taskCache";
import type { BackgroundTaskItem } from "@/tasks/types";
import buildAppTheme, { AppThemeProvider } from "@/theme";

const TEST_THEME = buildAppTheme("DARK");

export function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
      mutations: {
        retry: false,
      },
    },
  });
}

export function createAuthContextValue(
  overrides: Partial<AuthContextType> = {},
): AuthContextType {
  return {
    isAuthenticated: false,
    isInitialized: true,
    method: "session",
    privileged: false,
    refreshCapabilities: async () => ({}),
    sessionExpired: () => {},
    signIn: async () => {},
    signOut: async () => {},
    user: null,
    ...overrides,
  };
}

/** Seed the per-user capability cache entry that useCapabilityState reads. */
export function seedCapabilityCache(
  queryClient: QueryClient,
  overrides: Partial<CapabilityState> = {},
  userId = ANONYMOUS_USER_ID,
) {
  queryClient.setQueryData(
    capabilitiesQueryKey(userId),
    wireFromCapabilityState(overrides),
  );
}

// Config values now live in the query cache (read via useConfigValue's
// select slices); the context only carries actions. Tests get inert actions
// plus a seeded cache so components never have to know that.
export function createConfigContextValue(
  overrides: Partial<ConfigContextType> = {},
): ConfigContextType {
  return {
    isLoaded: true,
    setKey: () => {},
    updateConfig: () => {},
    ...overrides,
  };
}

/** Build the bridge and UI snapshots the slice hooks read, with overrides. */
export function buildTestConfigSnapshots(
  overrides: Partial<EffectiveAppSettings> = {},
): { bridge: AppConfig; ui: UIConfig } {
  const { showHiddenFiles = true, chunkSizeMB = 1, ...uiOverrides } = overrides;
  const ui: UIConfig = {
    primaryColor: "#2196f3",
    sidebarCollapsed: false,
    theme: "DARK",
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
    viewModes: {},
    viewModeDefault: "card",
    terminalFontSize: 16,
    ...uiOverrides,
    layoutOrders: uiOverrides.layoutOrders ?? {},
  };
  const bridge: AppConfig = {
    appSettings: { showHiddenFiles, chunkSizeMB },
    assistant: { agent: "" },
    docker: {
      folders: [],
      requireMountsForFolders: false,
    },
    jobs: {
      archiveCompressionWorkers: 0,
      archiveExtractWorkers: 0,
      notificationMinIntervalMs: 1000,
      progressMinBytesMB: 16,
      progressMinIntervalMs: 250,
    },
    storageMode: "home",
  };
  return { bridge, ui };
}

/** Seed the per-user config cache entries that useConfigValue subscribes to. */
export function seedConfigCache(
  queryClient: QueryClient,
  appSettings: Partial<EffectiveAppSettings> = {},
  userId = ANONYMOUS_USER_ID,
) {
  const { bridge, ui } = buildTestConfigSnapshots(appSettings);
  queryClient.setQueryData(bridgeConfigQueryKey(userId), bridge);
  queryClient.setQueryData(uiConfigQueryKey(userId), ui);
}

/** Seed the same task entries used by live streams, without mounting a mux. */
export function seedTaskCache(
  queryClient: QueryClient,
  items: BackgroundTaskItem[],
  userId = ANONYMOUS_USER_ID,
) {
  const tasks = createBackgroundTaskCache(queryClient, userId);
  tasks.downloads.set(items.filter((item) => item.type === "download"));
  tasks.uploads.set(items.filter((item) => item.type === "upload"));
  tasks.transfers.set(
    items.filter(
      (item) =>
        item.type === "compression" ||
        item.type === "extraction" ||
        item.type === "copy" ||
        item.type === "move",
    ),
  );
  tasks.indexers.set(items.filter((item) => item.type === "indexer"));
  tasks.backgroundTasks.set(items.filter((item) => item.type === "task"));
  return tasks;
}

interface AppRenderOptions extends Omit<RenderOptions, "wrapper"> {
  appSettings?: Partial<EffectiveAppSettings>;
  auth?: Partial<AuthContextType>;
  /** Capability flags to seed into the query cache (default: all unknown). */
  capabilities?: Partial<CapabilityState>;
  queryClient?: QueryClient;
  /** Disable for trees that mount the real ConfigProvider and own its cache. */
  seedConfig?: boolean;
}

export function render(
  ui: ReactElement,
  {
    appSettings,
    auth,
    capabilities,
    queryClient = createTestQueryClient(),
    seedConfig = true,
    ...options
  }: AppRenderOptions = {},
) {
  const authValue = createAuthContextValue(auth);
  if (seedConfig) {
    seedConfigCache(queryClient, appSettings, cacheUserId(authValue.user));
  }
  if (capabilities) {
    seedCapabilityCache(queryClient, capabilities, cacheUserId(authValue.user));
  }
  const configValue = createConfigContextValue();
  const user = userEvent.setup();

  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue}>
          <ConfigContext.Provider value={configValue}>
            <AppThemeProvider value={TEST_THEME}>{children}</AppThemeProvider>
          </ConfigContext.Provider>
        </AuthContext.Provider>
      </QueryClientProvider>
    );
  }

  return {
    user,
    ...rtlRender(ui, { wrapper: Wrapper, ...options }),
  };
}

export * from "@testing-library/react";

export interface TanStackMemoryRouterOptions {
  initialEntries?: string[];
}

const TanStackRouterTestChildren = createContext<ReactNode>(null);

function TanStackRouterTestRoute() {
  return useContext(TanStackRouterTestChildren);
}

export function createTanStackRouterWrapper({
  auth,
  capabilities,
  initialEntries = ["/"],
  queryClient = createTestQueryClient(),
}: TanStackMemoryRouterOptions & {
  auth?: Partial<AuthContextType>;
  capabilities?: Partial<CapabilityState>;
  queryClient?: QueryClient;
} = {}) {
  const authValue = createAuthContextValue(auth);
  seedConfigCache(queryClient, {}, cacheUserId(authValue.user));
  if (capabilities) {
    seedCapabilityCache(queryClient, capabilities, cacheUserId(authValue.user));
  }
  const rootRoute = createRootRoute({ component: TanStackRouterTestRoute });
  const router = createRouter({
    history: createMemoryHistory({ initialEntries }),
    routeTree: rootRoute,
  });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <TanStackRouterTestChildren.Provider value={children}>
        <QueryClientProvider client={queryClient}>
          <AuthContext.Provider value={authValue}>
            <ConfigContext.Provider value={createConfigContextValue()}>
              <AppThemeProvider value={TEST_THEME}>
                <RouterProvider router={router} />
              </AppThemeProvider>
            </ConfigContext.Provider>
          </AuthContext.Provider>
        </QueryClientProvider>
      </TanStackRouterTestChildren.Provider>
    );
  }

  return { router, Wrapper };
}

interface TanStackAppRenderOptions extends Omit<RenderOptions, "wrapper"> {
  auth?: Partial<AuthContextType>;
  capabilities?: Partial<CapabilityState>;
  queryClient?: QueryClient;
  tanstackRouter?: TanStackMemoryRouterOptions;
}

export function renderWithTanStackRouter(
  ui: ReactElement,
  {
    auth,
    capabilities,
    queryClient,
    tanstackRouter,
    ...options
  }: TanStackAppRenderOptions = {},
) {
  const user = userEvent.setup();
  const { router, Wrapper } = createTanStackRouterWrapper({
    auth,
    capabilities,
    queryClient,
    ...tanstackRouter,
  });

  return {
    router,
    user,
    ...rtlRender(ui, { wrapper: Wrapper, ...options }),
  };
}
