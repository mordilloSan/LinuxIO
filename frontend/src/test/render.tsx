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

import { emptyCapabilityState } from "@/api/capabilities";
import type { CapabilitiesResponse } from "@/api/capabilities";
import { AuthContext } from "@/contexts/AuthContext";
import buildAppTheme, { AppThemeProvider } from "@/theme";
import type { AuthContextType } from "@/types/auth";

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

interface AppRenderOptions extends Omit<RenderOptions, "wrapper"> {
  auth?: Partial<AuthContextType>;
  queryClient?: QueryClient;
}

export function render(
  ui: ReactElement,
  {
    auth,
    queryClient = createTestQueryClient(),
    ...options
  }: AppRenderOptions = {},
) {
  const authValue = createAuthContextValue(auth);
  const user = userEvent.setup();

  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue}>
          <AppThemeProvider value={buildAppTheme("DARK")}>
            {children}
          </AppThemeProvider>
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
            <AppThemeProvider value={buildAppTheme("DARK")}>
              <RouterProvider router={router} />
            </AppThemeProvider>
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
