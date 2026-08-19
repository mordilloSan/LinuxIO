import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthContext } from "@/contexts/AuthContext";
import { createAuthContextValue } from "@/test/render";

const routerMocks = vi.hoisted(() => ({
  contexts: [] as unknown[],
  invalidate: vi.fn(),
}));

vi.mock("./router", () => ({
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

const { default: ApplicationRouterProvider } = await import("./provider");

function ProviderHarness() {
  const [auth, setAuth] = useState(() =>
    createAuthContextValue({
      isAuthenticated: true,
      isInitialized: true,
      user: { id: "root", name: "root" },
    }),
  );

  return (
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
  );
}

describe("ApplicationRouterProvider", () => {
  beforeEach(() => {
    routerMocks.contexts.length = 0;
    routerMocks.invalidate.mockReset();
  });

  it("waits for auth initialization before mounting the router", () => {
    render(
      <AuthContext.Provider
        value={createAuthContextValue({ isInitialized: false })}
      >
        <ApplicationRouterProvider />
      </AuthContext.Provider>,
    );

    expect(screen.queryByTestId("router-provider")).not.toBeInTheDocument();
  });

  it("injects live auth and access context into the singleton", async () => {
    render(<ProviderHarness />);

    expect(screen.getByTestId("router-provider")).toBeInTheDocument();
    expect(routerMocks.contexts.at(-1)).toMatchObject({
      access: { dockerAvailable: null, privileged: false },
      auth: {
        isAuthenticated: true,
        isInitialized: true,
        user: { id: "root", name: "root" },
      },
    });
    expect(routerMocks.contexts.at(-1)).not.toHaveProperty("queryClient");
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
