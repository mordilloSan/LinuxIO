import React from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";

import { render, screen, waitFor } from "@/test/render";

const apiMocks = vi.hoisted(() => ({
  closeStreamMux: vi.fn(),
  getCapabilities: vi.fn(),
  initStreamMux: vi.fn(() => ({
    addStatusListener: vi.fn(() => vi.fn()),
  })),
  redirectToSignIn: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("@/api", async () => {
  const capabilities =
    await vi.importActual<typeof import("@/api/capabilities")>(
      "@/api/capabilities",
    );
  return {
    ...capabilities,
    closeStreamMux: apiMocks.closeStreamMux,
    initStreamMux: apiMocks.initStreamMux,
    linuxio: {
      system: {
        get_capabilities: apiMocks.getCapabilities,
      },
    },
  };
});

vi.mock("@/utils/navigation", () => ({
  redirectToSignIn: apiMocks.redirectToSignIn,
}));

const { AuthProvider } = await import("@/contexts/AuthContext");
const useAuth = (await import("@/hooks/useAuth")).default;

function Consumer() {
  const auth = useAuth();
  return (
    <div>
      <div>
        {auth.user?.name ?? "none"}:{String(auth.isAuthenticated)}:
        {String(auth.privileged)}:{String(auth.dockerAvailable)}
      </div>
      <button onClick={() => void auth.signIn("miguel", "secret")}>
        sign in
      </button>
      <button onClick={() => void auth.signOut()}>sign out</button>
    </div>
  );
}

function renderAuthProvider() {
  return render(
    <AuthProvider>
      <Consumer />
    </AuthProvider>,
  );
}

describe("AuthContext", () => {
  beforeEach(() => {
    apiMocks.closeStreamMux.mockClear();
    apiMocks.getCapabilities.mockReset();
    apiMocks.initStreamMux.mockClear();
    apiMocks.redirectToSignIn.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("initializes from stored user state", async () => {
    localStorage.setItem("auth_username", "miguel");
    localStorage.setItem("auth_privileged", "true");
    localStorage.setItem(
      "auth_capabilities",
      JSON.stringify({ dockerAvailable: true }),
    );

    renderAuthProvider();

    expect(
      await screen.findByText("miguel:true:true:true"),
    ).toBeInTheDocument();
    expect(apiMocks.initStreamMux).toHaveBeenCalledTimes(1);
  });

  it("persists successful sign-in state", async () => {
    vi.mocked(fetch).mockResolvedValue({
      json: async () => ({
        docker_available: true,
        privileged: true,
        success: true,
        update: { available: true, current_version: "1.0.0" },
      }),
      ok: true,
    } as Response);

    const { user } = renderAuthProvider();
    await user.click(screen.getByRole("button", { name: "sign in" }));

    await waitFor(() =>
      expect(screen.getByText("miguel:true:true:true")).toBeInTheDocument(),
    );
    expect(localStorage.getItem("auth_username")).toBe("miguel");
    expect(localStorage.getItem("auth_privileged")).toBe("true");
    expect(sessionStorage.getItem("update_info")).toContain("1.0.0");
  });

  it("clears local state on sign-out", async () => {
    localStorage.setItem("auth_username", "miguel");
    localStorage.setItem("auth_privileged", "true");
    sessionStorage.setItem("update_info", "{}");
    vi.mocked(fetch).mockResolvedValue({ ok: true } as Response);

    const { user } = renderAuthProvider();
    expect(
      await screen.findByText("miguel:true:true:null"),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "sign out" }));

    await waitFor(() =>
      expect(localStorage.getItem("auth_username")).toBeNull(),
    );
    expect(sessionStorage.getItem("update_info")).toBeNull();
    expect(apiMocks.redirectToSignIn).toHaveBeenCalledTimes(1);
  });
});
