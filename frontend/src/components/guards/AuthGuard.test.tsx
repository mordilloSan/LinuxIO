import type { ReactNode } from "react";
import { Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";
import { createAuthContextValue } from "@/test/render";

const useAuthMock = vi.hoisted(() => vi.fn());

vi.mock("@/hooks/useAuth", () => ({
  default: useAuthMock,
}));

vi.mock("@/contexts/AuthRuntimeProvider", () => ({
  default: ({ children }: { children: ReactNode }) => (
    <div data-testid="runtime">{children}</div>
  ),
}));

const { AuthGuard } = await import("@/components/guards/AuthGuard");

function SignInLocation() {
  const location = useLocation();
  return <div>sign-in:{location.search}</div>;
}

describe("AuthGuard", () => {
  beforeEach(() => {
    useAuthMock.mockReset();
  });

  it("renders nothing while auth is initializing", () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({ isInitialized: false }),
    );

    render(<AuthGuard>secret</AuthGuard>);

    expect(document.querySelector(".page-loader")).not.toBeInTheDocument();
    expect(screen.queryByText("secret")).not.toBeInTheDocument();
  });

  it("redirects unauthenticated users with a return URL", async () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: false,
        isInitialized: true,
      }),
    );

    render(
      <Routes>
        <Route
          path="/secret"
          element={
            <AuthGuard>
              <div>secret</div>
            </AuthGuard>
          }
        />
        <Route path="/sign-in" element={<SignInLocation />} />
      </Routes>,
      {
        memoryRouter: {
          initialEntries: ["/secret?tab=logs#latest"],
        },
      },
    );

    expect(
      await screen.findByText(
        "sign-in:?redirect=%2Fsecret%3Ftab%3Dlogs%23latest",
      ),
    ).toBeInTheDocument();
  });

  it("preserves an existing redirect target instead of nesting it", async () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: false,
        isInitialized: true,
      }),
    );

    render(
      <Routes>
        <Route
          path="/secret"
          element={
            <AuthGuard>
              <div>secret</div>
            </AuthGuard>
          }
        />
        <Route path="/sign-in" element={<SignInLocation />} />
      </Routes>,
      {
        memoryRouter: {
          initialEntries: [
            "/secret?redirect=%2Ffilebrowser%2Fsrv%2Fmy%2520files%3Fview%3Ddetails%23preview",
          ],
        },
      },
    );

    expect(
      await screen.findByText(
        "sign-in:?redirect=%2Ffilebrowser%2Fsrv%2Fmy%2520files%3Fview%3Ddetails%23preview",
      ),
    ).toBeInTheDocument();
  });

  it("passes through the sign-in route when unauthenticated", () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: false,
        isInitialized: true,
      }),
    );

    render(
      <Routes>
        <Route element={<AuthGuard />}>
          <Route path="/sign-in" element={<div>sign-in form</div>} />
        </Route>
      </Routes>,
      {
        memoryRouter: {
          initialEntries: ["/sign-in"],
        },
      },
    );

    expect(screen.getByText("sign-in form")).toBeInTheDocument();
  });

  it("renders authenticated children inside runtime provider", async () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: true,
        isInitialized: true,
        user: { id: "miguel", name: "miguel" },
      }),
    );

    render(
      <AuthGuard>
        <div>secret</div>
      </AuthGuard>,
    );

    expect(await screen.findByTestId("runtime")).toContainElement(
      screen.getByText("secret"),
    );
  });
});
