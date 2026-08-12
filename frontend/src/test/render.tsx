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

import type { AppConfig } from "@/api";
import { emptyCapabilityState } from "@/api/capabilities";
import type { CapabilitiesResponse } from "@/api/capabilities";
import { AuthContext } from "@/contexts/AuthContext";
import { ConfigContext } from "@/contexts/ConfigContext";
import buildAppTheme, { AppThemeProvider } from "@/theme";
import type { AuthContextType } from "@/types/auth";
import type { ConfigContextType } from "@/types/config";

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
    ...emptyCapabilityState,
    isAuthenticated: false,
    isInitialized: true,
    method: "session",
    privileged: false,
    refreshCapabilities: async () => ({}) as CapabilitiesResponse,
    sessionExpired: () => {},
    signIn: async () => {},
    signOut: async () => {},
    user: null,
    ...overrides,
  };
}

// Anything rendering a list reads config now: reorderable surfaces read their
// saved order from it. Tests get a real context with inert writes so they never
// have to know that.
export function createConfigContextValue(
  overrides: Partial<AppConfig["appSettings"]> = {},
): ConfigContextType {
  return {
    config: {
      appSettings: {
        primaryColor: "#2196f3",
        showHiddenFiles: true,
        sidebarCollapsed: false,
        theme: "DARK",
        ...overrides,
      },
      docker: {
        folders: [],
        proxy: { caddyEnabled: false },
        requireMountsForFolders: false,
      },
      jobs: {
        archiveCompressionWorkers: 0,
        archiveExtractWorkers: 0,
        heavyArchiveConcurrency: 1,
        notificationMinIntervalMs: 1000,
        progressMinBytesMB: 16,
        progressMinIntervalMs: 250,
      },
    },
    isLoaded: true,
    setKey: () => {},
    updateConfig: () => {},
  };
}

interface AppRenderOptions extends Omit<RenderOptions, "wrapper"> {
  appSettings?: Partial<AppConfig["appSettings"]>;
  auth?: Partial<AuthContextType>;
  queryClient?: QueryClient;
}

export function render(
  ui: ReactElement,
  {
    appSettings,
    auth,
    queryClient = createTestQueryClient(),
    ...options
  }: AppRenderOptions = {},
) {
  const authValue = createAuthContextValue(auth);
  const configValue = createConfigContextValue(appSettings);
  const user = userEvent.setup();

  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue}>
          <ConfigContext.Provider value={configValue}>
            <AppThemeProvider value={buildAppTheme("DARK")}>
              {children}
            </AppThemeProvider>
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
  initialEntries = ["/"],
  queryClient = createTestQueryClient(),
}: TanStackMemoryRouterOptions & {
  auth?: Partial<AuthContextType>;
  queryClient?: QueryClient;
} = {}) {
  const authValue = createAuthContextValue(auth);
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
              <AppThemeProvider value={buildAppTheme("DARK")}>
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
  queryClient?: QueryClient;
  tanstackRouter?: TanStackMemoryRouterOptions;
}

export function renderWithTanStackRouter(
  ui: ReactElement,
  {
    auth,
    queryClient,
    tanstackRouter,
    ...options
  }: TanStackAppRenderOptions = {},
) {
  const user = userEvent.setup();
  const { router, Wrapper } = createTanStackRouterWrapper({
    auth,
    queryClient,
    ...tanstackRouter,
  });

  return {
    router,
    user,
    ...rtlRender(ui, { wrapper: Wrapper, ...options }),
  };
}
