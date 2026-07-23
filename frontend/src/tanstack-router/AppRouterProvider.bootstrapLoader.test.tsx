import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import BootstrapLoaderReady from "@/components/loaders/BootstrapLoaderReady";
import { createAuthContextValue } from "@/test/render";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((resolvePromise) => {
    resolve = resolvePromise;
  });

  return { promise, resolve };
}

describe("AppRouterProvider bootstrap loader", () => {
  afterEach(() => {
    document.getElementById("app-bootstrap-loader")?.remove();
    document.getElementById("root")?.remove();
    vi.doUnmock("@/routes");
    vi.resetModules();
    window.history.replaceState(null, "", "/");
  });

  it("keeps the bootstrap loader until the lazy sign-in screen resolves", async () => {
    const signIn = deferred();
    let signInStarted = false;

    vi.resetModules();
    vi.doMock("@/routes", async () => {
      const { lazyRouteComponent, Outlet } =
        await import("@tanstack/react-router");
      const SignIn = lazyRouteComponent(async () => {
        signInStarted = true;
        await signIn.promise;
        return {
          default: () => (
            <>
              <form data-testid="sign-in-form">Sign in</form>
              <BootstrapLoaderReady />
            </>
          ),
        };
      });

      return {
        AuthenticatedLayout: Outlet,
        Page404: () => <div>not found</div>,
        protectedRouteComponents: {},
        SignIn,
      };
    });

    const [{ AuthContext }, { default: AppRouterProvider }] = await Promise.all(
      [
        import("@/contexts/AuthContext"),
        import("@/tanstack-router/AppRouterProvider"),
      ],
    );
    const bootstrapLoader = document.createElement("div");
    bootstrapLoader.id = "app-bootstrap-loader";
    document.body.append(bootstrapLoader);
    const root = document.createElement("div");
    root.id = "root";
    root.setAttribute("aria-busy", "true");
    root.setAttribute("inert", "");
    document.body.append(root);

    render(
      <QueryClientProvider client={new QueryClient()}>
        <AuthContext.Provider
          value={createAuthContextValue({
            isAuthenticated: false,
            isInitialized: true,
          })}
        >
          <AppRouterProvider />
        </AuthContext.Provider>
      </QueryClientProvider>,
      { container: root },
    );

    await waitFor(() => expect(signInStarted).toBe(true));
    expect(bootstrapLoader).toBeInTheDocument();
    expect(root).toHaveAttribute("aria-busy", "true");
    expect(root).toHaveAttribute("inert");

    signIn.resolve();

    expect(await screen.findByTestId("sign-in-form")).toBeInTheDocument();
    await waitFor(() => expect(bootstrapLoader).not.toBeInTheDocument());
    expect(root).not.toHaveAttribute("aria-busy");
    expect(root).not.toHaveAttribute("inert");
  });
});
