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
  return <div>location:{location.pathname}</div>;
}

describe("GuestGuard", () => {
  beforeEach(() => {
    useAuthMock.mockReset();
  });

  it("renders children while auth is still initializing", () => {
    useAuthMock.mockReturnValue(
      createAuthContextValue({
        isAuthenticated: false,
        isInitialized: false,
      }),
    );

    render(
      <GuestGuard>
        <div>sign-in form</div>
      </GuestGuard>,
    );

    expect(screen.getByText("sign-in form")).toBeInTheDocument();
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

  it("redirects authenticated users to redirect query target", async () => {
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
        <Route path="/docker" element={<LocationProbe />} />
      </Routes>,
      {
        memoryRouter: {
          initialEntries: ["/sign-in?redirect=/docker"],
        },
      },
    );

    expect(await screen.findByText("location:/docker")).toBeInTheDocument();
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
