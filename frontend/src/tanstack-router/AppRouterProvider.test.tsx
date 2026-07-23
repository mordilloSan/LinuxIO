import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { type ReactNode, useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AuthContext } from "@/contexts/AuthContext";
import { createAuthContextValue } from "@/test/render";
import type { AuthContextType } from "@/types/auth";

vi.mock("@/contexts/AuthRuntimeProvider", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/routes", async () => {
  const { Outlet, useLocation } = await import("@tanstack/react-router");

  function LocationMarker({ id }: { id: string }) {
    const location = useLocation();
    return <div data-testid={`route-${id}`}>{location.href}</div>;
  }

  return {
    AuthenticatedLayout: Outlet,
    Page404: () => <div data-testid="not-found">not-found</div>,
    protectedRouteComponents: {
      dashboard: () => <LocationMarker id="dashboard" />,
      docker: () => <LocationMarker id="docker" />,
      filebrowser: () => <LocationMarker id="filebrowser" />,
      network: () => <LocationMarker id="network" />,
    },
    SignIn: () => <LocationMarker id="sign-in" />,
  };
});

const { default: AppRouterProvider } =
  await import("@/tanstack-router/AppRouterProvider");

interface RouterHarnessProps {
  initialAuth: Partial<AuthContextType>;
  updatedAuth: Partial<AuthContextType>;
}

function RouterHarness({ initialAuth, updatedAuth }: RouterHarnessProps) {
  const [queryClient] = useState(() => new QueryClient());
  const [auth, setAuth] = useState(() => createAuthContextValue(initialAuth));

  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <button
          onClick={() => setAuth(createAuthContextValue(updatedAuth))}
          type="button"
        >
          update auth
        </button>
        <AppRouterProvider />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

const signedOut = {
  isAuthenticated: false,
  isInitialized: true,
};

const signedIn = {
  isAuthenticated: true,
  isInitialized: true,
  privileged: true,
  user: { id: "root", name: "root" },
};

describe("AppRouterProvider", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/");
  });

  afterEach(() => {
    window.history.replaceState(null, "", "/");
  });

  it("restores an unauthenticated deep link after AuthContext signs in", async () => {
    window.history.replaceState(
      null,
      "",
      "/filebrowser/srv/my%20files?tail=200&enabled=true#preview",
    );
    render(<RouterHarness initialAuth={signedOut} updatedAuth={signedIn} />);

    expect(await screen.findByTestId("route-sign-in")).toHaveTextContent(
      "/sign-in?redirect=%2Ffilebrowser%2Fsrv%2Fmy%2520files%3Ftail%3D200%26enabled%3Dtrue%23preview",
    );

    fireEvent.click(screen.getByRole("button", { name: "update auth" }));

    expect(await screen.findByTestId("route-filebrowser")).toHaveTextContent(
      "/filebrowser/srv/my%20files?tail=200&enabled=true#preview",
    );
    expect(window.location.pathname).toBe("/filebrowser/srv/my%20files");
    expect(window.location.search).toBe("?tail=200&enabled=true");
    expect(window.location.hash).toBe("#preview");
  });

  it("re-evaluates protected access when AuthContext capabilities change", async () => {
    window.history.replaceState(null, "", "/docker");
    render(
      <RouterHarness
        initialAuth={signedIn}
        updatedAuth={{ ...signedIn, dockerAvailable: true }}
      />,
    );

    expect(await screen.findByTestId("not-found")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "update auth" }));
    expect(await screen.findByTestId("route-docker")).toHaveTextContent(
      "/docker",
    );
  });
});
