import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthContext } from "@/contexts/AuthContext";
import { createAuthContextValue } from "@/test/render";

const routerMocks = vi.hoisted(() => ({
  contexts: [] as unknown[],
  invalidate: vi.fn(),
}));

vi.mock("@/router", () => ({
  router: {
    invalidate: routerMocks.invalidate,
  },
}));

vi.mock("@tanstack/react-router", async () => {
  const actual = await vi.importActual("@tanstack/react-router");
  return {
    ...actual,
    RouterProvider: ({ context }: { context: unknown }) => {
      routerMocks.contexts.push(context);
      return <div data-testid="router-provider" />;
    },
  };
});

const { default: ApplicationRouterProvider } =
  await import("@/routes/-provider");

function ProviderHarness() {
  const [queryClient] = useState(() => new QueryClient());
  const [auth, setAuth] = useState(() =>
    createAuthContextValue({
      isAuthenticated: true,
      isInitialized: true,
      user: { id: "root", name: "root" },
    }),
  );

  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <button
          onClick={() =>
            setAuth(
              createAuthContextValue({
                dockerAvailable: true,
                isAuthenticated: true,
                isInitialized: true,
                user: { id: "root", name: "root" },
              }),
            )
          }
          type="button"
        >
          update auth
        </button>
        <ApplicationRouterProvider />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

describe("ApplicationRouterProvider", () => {
  beforeEach(() => {
    routerMocks.contexts.length = 0;
    routerMocks.invalidate.mockReset();
  });

  it("waits for auth initialization before mounting the router", () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <AuthContext.Provider
          value={createAuthContextValue({ isInitialized: false })}
        >
          <ApplicationRouterProvider />
        </AuthContext.Provider>
      </QueryClientProvider>,
    );

    expect(screen.queryByTestId("router-provider")).not.toBeInTheDocument();
  });

  it("injects live auth, access, and Query context into the singleton", async () => {
    render(<ProviderHarness />);

    expect(screen.getByTestId("router-provider")).toBeInTheDocument();
    expect(routerMocks.contexts.at(-1)).toMatchObject({
      access: { dockerAvailable: null, privileged: false },
      auth: {
        isAuthenticated: true,
        isInitialized: true,
        user: { id: "root", name: "root" },
      },
      queryClient: expect.any(QueryClient),
    });
    expect(routerMocks.invalidate).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "update auth" }));

    await waitFor(() => {
      expect(routerMocks.contexts.at(-1)).toMatchObject({
        access: { dockerAvailable: true },
      });
      expect(routerMocks.invalidate).toHaveBeenCalledTimes(1);
    });
  });
});
