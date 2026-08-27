import { describe, expect, it, vi, beforeEach } from "vitest";

import { act, render, screen, waitFor } from "@/test/render";
import { readSigninNotice } from "@/utils/signinNotice";

const apiMocks = vi.hoisted(() => ({
  call: vi.fn(),
  closeStreamMux: vi.fn(),
  getCapabilities: vi.fn(),
  initStreamMux: vi.fn(() => ({
    status: "connecting" as "connecting" | "open" | "closed" | "error",
    addStatusListener: vi.fn(
      (listener: (status: "open" | "closed" | "error") => void) => {
        void listener;
        return vi.fn();
      },
    ),
  })),
  redirectToSignIn: vi.fn(),
}));

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: {
    error: toastMocks.error,
    success: toastMocks.success,
  },
}));

vi.mock("@/api", async () => {
  const capabilities =
    await vi.importActual<typeof import("@/api/capabilities")>(
      "@/api/capabilities",
    );
  return {
    ...capabilities,
    call: apiMocks.call,
    closeStreamMux: apiMocks.closeStreamMux,
    initStreamMux: apiMocks.initStreamMux,
    linuxio: {
      system: {
        get_capabilities: {
          route: "system.get_capabilities",
          queryKey: ["linuxio", "system", "get_capabilities"],
          queryFn: () => apiMocks.call("system.get_capabilities"),
        },
      },
    },
  };
});

vi.mock("@/utils/navigation", () => ({
  redirectToSignIn: apiMocks.redirectToSignIn,
}));

const { AuthProvider } = await import("@/contexts/AuthContext");
const useAuth = (await import("@/hooks/useAuth")).default;
const { useCapabilityState } = await import("@/hooks/useCapabilities");

function Consumer() {
  const auth = useAuth();
  const { dockerAvailable } = useCapabilityState();
  return (
    <div>
      <div>
        {auth.user?.name ?? "none"}:{String(auth.isAuthenticated)}:
        {String(auth.privileged)}:{String(dockerAvailable)}
      </div>
      <button onClick={() => void auth.signIn("miguel", "secret")}>
        sign in
      </button>
      <button onClick={() => void auth.signOut()}>sign out</button>
      <button onClick={() => void auth.refreshCapabilities()}>refresh</button>
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
    apiMocks.call.mockReset();
    apiMocks.closeStreamMux.mockClear();
    apiMocks.getCapabilities.mockReset();
    apiMocks.call.mockImplementation(() => apiMocks.getCapabilities());
    apiMocks.initStreamMux.mockReset();
    apiMocks.initStreamMux.mockReturnValue({
      status: "connecting",
      addStatusListener: vi.fn(() => vi.fn()),
    });
    apiMocks.redirectToSignIn.mockClear();
    toastMocks.error.mockClear();
    toastMocks.success.mockClear();
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

  it("persists successful sign-in state without login update data", async () => {
    localStorage.setItem(
      "auth_capabilities",
      JSON.stringify({ dockerAvailable: true }),
    );
    vi.mocked(fetch).mockResolvedValue({
      json: async () => ({
        privileged: true,
        success: true,
      }),
      ok: true,
    } as Response);

    const { user } = renderAuthProvider();
    await user.click(screen.getByRole("button", { name: "sign in" }));

    await waitFor(() =>
      expect(screen.getByText("miguel:true:true:null")).toBeInTheDocument(),
    );
    expect(localStorage.getItem("auth_username")).toBe("miguel");
    expect(localStorage.getItem("auth_privileged")).toBe("true");
    expect(localStorage.getItem("auth_capabilities")).toBeNull();
    expect(sessionStorage.getItem("update_info")).toBeNull();
  });

  it("refreshes and persists capabilities when the mux is already open", async () => {
    apiMocks.initStreamMux.mockReturnValue({
      status: "open",
      addStatusListener: vi.fn(() => vi.fn()),
    });
    vi.mocked(fetch).mockResolvedValue({
      json: async () => ({ privileged: true, success: true }),
      ok: true,
    } as Response);
    apiMocks.getCapabilities.mockResolvedValue({ docker_available: true });

    const { user } = renderAuthProvider();
    await user.click(screen.getByRole("button", { name: "sign in" }));
    await waitFor(() =>
      expect(screen.getByText("miguel:true:true:true")).toBeInTheDocument(),
    );
    expect(apiMocks.getCapabilities).toHaveBeenCalledTimes(1);
    expect(
      JSON.parse(localStorage.getItem("auth_capabilities")!),
    ).toMatchObject({ dockerAvailable: true });
  });

  it("deduplicates repeated open notifications while a scan is in flight", async () => {
    const statusListeners: Array<
      (status: "open" | "closed" | "error") => void
    > = [];
    apiMocks.initStreamMux.mockReturnValue({
      status: "connecting",
      addStatusListener: vi.fn((listener) => {
        statusListeners.push(listener);
        return vi.fn();
      }),
    });
    vi.mocked(fetch).mockResolvedValue({
      json: async () => ({ privileged: true, success: true }),
      ok: true,
    } as Response);
    let resolveCapabilities!: (value: { docker_available: boolean }) => void;
    apiMocks.getCapabilities.mockReturnValue(
      new Promise((resolve) => {
        resolveCapabilities = resolve;
      }),
    );

    const { user } = renderAuthProvider();
    await user.click(screen.getByRole("button", { name: "sign in" }));
    await waitFor(() => expect(statusListeners).toHaveLength(1));
    act(() => {
      statusListeners[0]("open");
      statusListeners[0]("open");
    });
    expect(apiMocks.getCapabilities).toHaveBeenCalledTimes(1);
    await act(async () => {
      resolveCapabilities({ docker_available: true });
      await Promise.resolve();
    });
    await waitFor(() =>
      expect(screen.getByText("miguel:true:true:true")).toBeInTheDocument(),
    );
  });

  it("ignores a capability result that completes after logout", async () => {
    const statusListeners: Array<
      (status: "open" | "closed" | "error") => void
    > = [];
    apiMocks.initStreamMux.mockReturnValue({
      status: "connecting",
      addStatusListener: vi.fn((listener) => {
        statusListeners.push(listener);
        return vi.fn();
      }),
    });
    vi.mocked(fetch).mockResolvedValue({
      json: async () => ({ privileged: true, success: true }),
      ok: true,
    } as Response);
    let resolveCapabilities!: (value: { docker_available: boolean }) => void;
    apiMocks.getCapabilities.mockReturnValue(
      new Promise((resolve) => {
        resolveCapabilities = resolve;
      }),
    );

    const { user } = renderAuthProvider();
    await user.click(screen.getByRole("button", { name: "sign in" }));
    await waitFor(() => expect(statusListeners).toHaveLength(1));
    act(() => statusListeners[0]("open"));
    await user.click(screen.getByRole("button", { name: "sign out" }));
    await act(async () => {
      resolveCapabilities({ docker_available: true });
      await Promise.resolve();
    });

    await waitFor(() =>
      expect(screen.getByText("none:false:false:null")).toBeInTheDocument(),
    );
    expect(localStorage.getItem("auth_capabilities")).toBeNull();
  });

  it("ignores a manual capability refresh that completes after logout", async () => {
    vi.mocked(fetch).mockResolvedValue({
      json: async () => ({ privileged: true, success: true }),
      ok: true,
    } as Response);
    let resolveCapabilities!: (value: { docker_available: boolean }) => void;
    apiMocks.getCapabilities.mockReturnValue(
      new Promise((resolve) => {
        resolveCapabilities = resolve;
      }),
    );

    const { user } = renderAuthProvider();
    await user.click(screen.getByRole("button", { name: "sign in" }));
    await user.click(screen.getByRole("button", { name: "refresh" }));
    expect(apiMocks.getCapabilities).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole("button", { name: "sign out" }));
    await act(async () => {
      resolveCapabilities({ docker_available: true });
      await Promise.resolve();
    });

    await waitFor(() =>
      expect(screen.getByText("none:false:false:null")).toBeInTheDocument(),
    );
    expect(localStorage.getItem("auth_capabilities")).toBeNull();
  });

  it("ignores a manual capability refresh that completes after unmount", async () => {
    vi.mocked(fetch).mockResolvedValue({
      json: async () => ({ privileged: true, success: true }),
      ok: true,
    } as Response);
    let resolveCapabilities!: (value: { docker_available: boolean }) => void;
    apiMocks.getCapabilities.mockReturnValue(
      new Promise((resolve) => {
        resolveCapabilities = resolve;
      }),
    );

    const { unmount, user } = renderAuthProvider();
    await user.click(screen.getByRole("button", { name: "sign in" }));
    await user.click(screen.getByRole("button", { name: "refresh" }));
    expect(apiMocks.getCapabilities).toHaveBeenCalledTimes(1);
    unmount();
    await act(async () => {
      resolveCapabilities({ docker_available: true });
      await Promise.resolve();
    });
    expect(localStorage.getItem("auth_capabilities")).toBeNull();
  });

  it("retries an automatic capability refresh after a later open", async () => {
    const statusListeners: Array<
      (status: "open" | "closed" | "error") => void
    > = [];
    apiMocks.initStreamMux.mockReturnValue({
      status: "connecting",
      addStatusListener: vi.fn((listener) => {
        statusListeners.push(listener);
        return vi.fn();
      }),
    });
    vi.mocked(fetch).mockResolvedValue({
      json: async () => ({ privileged: true, success: true }),
      ok: true,
    } as Response);
    apiMocks.getCapabilities
      .mockRejectedValueOnce(new Error("scan failed"))
      .mockResolvedValueOnce({ docker_available: true });

    const { user } = renderAuthProvider();
    await user.click(screen.getByRole("button", { name: "sign in" }));
    await waitFor(() => expect(statusListeners).toHaveLength(1));
    act(() => statusListeners[0]("open"));
    await waitFor(() =>
      expect(apiMocks.getCapabilities).toHaveBeenCalledTimes(1),
    );
    await act(async () => {
      await Promise.resolve();
    });
    act(() => statusListeners[0]("open"));

    await waitFor(() =>
      expect(screen.getByText("miguel:true:true:true")).toBeInTheDocument(),
    );
    expect(apiMocks.getCapabilities).toHaveBeenCalledTimes(2);
  });

  it("clears local state on sign-out", async () => {
    localStorage.setItem("auth_username", "miguel");
    localStorage.setItem("auth_privileged", "true");
    sessionStorage.setItem("update_info", "{}");
    sessionStorage.setItem("update_info_checked", "true");
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
    expect(sessionStorage.getItem("update_info_checked")).toBeNull();
    expect(localStorage.getItem("logout")).not.toBeNull();
    expect(localStorage.getItem("session_expired")).toBeNull();
    expect(apiMocks.redirectToSignIn).toHaveBeenCalledTimes(1);
    // Deliberate sign-out must not preserve the current path.
    expect(apiMocks.redirectToSignIn).toHaveBeenCalledWith(false);
  });

  it("handles a real session-timeout signal from the stream mux", async () => {
    localStorage.setItem("auth_username", "miguel");
    localStorage.setItem("auth_privileged", "true");
    sessionStorage.setItem("update_info", "{}");
    sessionStorage.setItem("update_info_checked", "true");
    const statusListeners: Array<
      (status: "open" | "closed" | "error") => void
    > = [];
    const unsubscribe = vi.fn();
    apiMocks.initStreamMux.mockReturnValue({
      status: "connecting",
      addStatusListener: vi.fn(
        (listener: (status: "open" | "closed" | "error") => void) => {
          statusListeners.push(listener);
          return unsubscribe;
        },
      ),
    });
    const consoleLog = vi
      .spyOn(console, "log")
      .mockImplementation(() => undefined);

    renderAuthProvider();

    expect(
      await screen.findByText("miguel:true:true:null"),
    ).toBeInTheDocument();

    statusListeners[0]("error");

    await waitFor(() =>
      expect(screen.getByText("none:false:false:null")).toBeInTheDocument(),
    );
    // Involuntary expiry leaves a one-shot notice for the sign-in screen,
    // instead of a toast that the redirect would discard.
    expect(readSigninNotice()).toBe("expired");
    expect(localStorage.getItem("auth_username")).toBeNull();
    expect(localStorage.getItem("auth_privileged")).toBeNull();
    expect(sessionStorage.getItem("update_info")).toBeNull();
    expect(sessionStorage.getItem("update_info_checked")).toBeNull();
    expect(localStorage.getItem("session_expired")).not.toBeNull();
    expect(apiMocks.redirectToSignIn).toHaveBeenCalledTimes(1);
    // Involuntary session loss must preserve the current path for post-login return.
    expect(apiMocks.redirectToSignIn).toHaveBeenCalledWith(true);
    expect(apiMocks.closeStreamMux).toHaveBeenCalled();
    expect(consoleLog).toHaveBeenCalledWith(
      "[AuthContext] Session invalid or expired",
    );
  });

  it("preserves this tab's path for a broadcast session expiry", async () => {
    localStorage.setItem("auth_username", "miguel");
    localStorage.setItem("auth_privileged", "true");
    sessionStorage.setItem("update_info", "{}");
    sessionStorage.setItem("update_info_checked", "true");

    renderAuthProvider();

    expect(
      await screen.findByText("miguel:true:true:null"),
    ).toBeInTheDocument();

    act(() => {
      window.dispatchEvent(
        new StorageEvent("storage", {
          key: "session_expired",
          newValue: String(Date.now()),
        }),
      );
    });

    await waitFor(() =>
      expect(screen.getByText("none:false:false:null")).toBeInTheDocument(),
    );
    expect(readSigninNotice()).toBe("expired");
    expect(sessionStorage.getItem("update_info")).toBeNull();
    expect(sessionStorage.getItem("update_info_checked")).toBeNull();
    expect(localStorage.getItem("session_expired")).toBeNull();
    expect(apiMocks.redirectToSignIn).toHaveBeenCalledTimes(1);
    expect(apiMocks.redirectToSignIn).toHaveBeenCalledWith(true);
  });
});
