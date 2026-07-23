import { lazy } from "react";
import { Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { GuestGuard } from "@/components/guards/GuestGuard";
import { createAuthContextValue, render, screen } from "@/test/render";

const useAuthMock = vi.hoisted(() => vi.fn());

vi.mock("@/hooks/useAuth", () => ({
  default: useAuthMock,
}));

function LocationProbe() {
  const location = useLocation();
  return (
    <div>
      location:{location.pathname}
      {location.search}
      {location.hash}
    </div>
  );
}

describe("GuestGuard", () => {
  beforeEach(() => {
    useAuthMock.mockReset();
  });

  it("renders nothing while auth is still initializing", () => {
    const loadLogin = vi.fn(async () => ({
      default: () => <div>sign-in form</div>,
    }));
    const LoginProbe = lazy(loadLogin);
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: false,
        isInitialized: false,
      }),
    );

    render(
      <GuestGuard>
        <LoginProbe />
      </GuestGuard>,
    );

    expect(loadLogin).not.toHaveBeenCalled();
    expect(screen.queryByText("sign-in form")).not.toBeInTheDocument();
  });

  it("renders children for initialized guests", () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: false,
        isInitialized: true,
      }),
    );

    render(
      <GuestGuard>
        <div>sign-in form</div>
      </GuestGuard>,
    );

    expect(screen.getByText("sign-in form")).toBeInTheDocument();
  });

  it("redirects authenticated users to the complete redirect query target", async () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: true,
        isInitialized: true,
      }),
    );

    render(
      <Routes>
        <Route
          path="/sign-in"
          element={
            <GuestGuard>
              <div>sign-in form</div>
            </GuestGuard>
          }
        />
        <Route path="/filebrowser/*" element={<LocationProbe />} />
      </Routes>,
      {
        memoryRouter: {
          initialEntries: [
            "/sign-in?redirect=%2Ffilebrowser%2Fsrv%2Fmy%2520files%3Fview%3Ddetails%23preview",
          ],
        },
      },
    );

    expect(
      await screen.findByText(
        "location:/filebrowser/srv/my%20files?view=details#preview",
      ),
    ).toBeInTheDocument();
  });

  it("redirects authenticated users to dashboard by default", async () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: true,
        isInitialized: true,
      }),
    );

    render(
      <Routes>
        <Route
          path="/sign-in"
          element={
            <GuestGuard>
              <div>sign-in form</div>
            </GuestGuard>
          }
        />
        <Route path="/" element={<LocationProbe />} />
      </Routes>,
      {
        memoryRouter: {
          initialEntries: ["/sign-in"],
        },
      },
    );

    expect(await screen.findByText("location:/")).toBeInTheDocument();
  });
});
