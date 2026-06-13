import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  render as rtlRender,
  type RenderOptions,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import React from "react";
import { MemoryRouter, type MemoryRouterProps } from "react-router-dom";

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
    signIn: async () => {},
    signOut: async () => {},
    user: null,
    ...overrides,
  };
}

interface AppRenderOptions extends Omit<RenderOptions, "wrapper"> {
  auth?: Partial<AuthContextType>;
  memoryRouter?: MemoryRouterProps;
  queryClient?: QueryClient;
}

export function render(
  ui: React.ReactElement,
  {
    auth,
    memoryRouter,
    queryClient = createTestQueryClient(),
    ...options
  }: AppRenderOptions = {},
) {
  const authValue = createAuthContextValue(auth);
  const user = userEvent.setup();

  function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <MemoryRouter {...memoryRouter}>
        <QueryClientProvider client={queryClient}>
          <AuthContext.Provider value={authValue}>
            <AppThemeProvider value={buildAppTheme("DARK")}>
              {children}
            </AppThemeProvider>
          </AuthContext.Provider>
        </QueryClientProvider>
      </MemoryRouter>
    );
  }

  return {
    user,
    ...rtlRender(ui, { wrapper: Wrapper, ...options }),
  };
}

export * from "@testing-library/react";
