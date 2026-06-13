import { Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { PrivilegedGuard } from "@/components/guards/PrivilegedGuard";
import { createAuthContextValue, render, screen } from "@/test/render";

const useAuthMock = vi.hoisted(() => vi.fn());

vi.mock("@/hooks/useAuth", () => ({
  default: useAuthMock,
}));

function LocationProbe() {
  const location = useLocation();
  return <div>location:{location.pathname}</div>;
}

describe("PrivilegedGuard", () => {
  beforeEach(() => {
    useAuthMock.mockReset();
  });

  it("shows a loader while auth is initializing", () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isInitialized: false,
      }),
    );

    render(
      <PrivilegedGuard>
        <div>admin</div>
      </PrivilegedGuard>,
    );

    expect(document.querySelector(".page-loader")).toBeInTheDocument();
  });

  it("redirects initialized guests to sign-in", async () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: false,
        isInitialized: true,
      }),
    );

    render(
      <Routes>
        <Route
          path="/admin"
          element={
            <PrivilegedGuard>
              <div>admin</div>
            </PrivilegedGuard>
          }
        />
        <Route path="/sign-in" element={<LocationProbe />} />
      </Routes>,
      {
        memoryRouter: {
          initialEntries: ["/admin"],
        },
      },
    );

    expect(await screen.findByText("location:/sign-in")).toBeInTheDocument();
  });

  it("shows access denied for authenticated non-privileged users", () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: true,
        isInitialized: true,
        privileged: false,
      }),
    );

    render(
      <PrivilegedGuard>
        <div>admin</div>
      </PrivilegedGuard>,
    );

    expect(
      screen.getByText(
        "Access Denied: This page requires administrator privileges.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("admin")).not.toBeInTheDocument();
  });

  it("renders children for privileged users", () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: true,
        isInitialized: true,
        privileged: true,
      }),
    );

    render(
      <PrivilegedGuard>
        <div>admin</div>
      </PrivilegedGuard>,
    );

    expect(screen.getByText("admin")).toBeInTheDocument();
  });
});
