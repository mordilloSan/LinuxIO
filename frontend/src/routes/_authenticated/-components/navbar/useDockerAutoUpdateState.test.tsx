import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => {
  const initialState = {
    available: true,
    containers: [],
    missing_container_names: [],
    options: {
      cleanup: false,
      container_names: [],
      enabled: true,
      mode: "update" as const,
      time: "04:00",
    },
    timer_active: false,
    timer_enabled: true,
  };
  return {
    cacheState: initialState,
    mutateAsync: vi.fn(),
    toastError: vi.fn(),
    toastSuccess: vi.fn(),
    queryClient: {
      cancelQueries: vi.fn().mockResolvedValue(undefined),
      getQueryData: vi.fn(() => mocks.cacheState),
      setQueryData: vi.fn((_: unknown, state: typeof initialState) => {
        mocks.cacheState = state;
      }),
    },
  };
});

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return { ...actual, useQueryClient: () => mocks.queryClient };
});

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  const { emptyCapabilityState } =
    await vi.importActual<typeof import("@/api/capabilities")>(
      "@/api/capabilities",
    );
  return {
    ...actual,
    emptyCapabilityState,
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        get_container_auto_update: {
          ...actual.linuxio.docker.get_container_auto_update,
          queryKey: ["docker.get_container_auto_update"],
          queryFn: async () => mocks.cacheState,
        },
        set_container_auto_update: {
          ...actual.linuxio.docker.set_container_auto_update,
        },
      },
    },
    useCallMutation: (endpoint: { route: string }) => {
      if (endpoint.route.endsWith("set_container_auto_update")) {
        return { mutateAsync: mocks.mutateAsync };
      }
      throw new Error(`Unexpected mutation route: ${endpoint.route}`);
    },
  };
});

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => ({
    error: mocks.toastError,
    success: mocks.toastSuccess,
  }),
}));

const { act, createTestQueryClient, renderHook, waitFor } =
  await import("@/test/render");
const { QueryClientProvider } = await import("@tanstack/react-query");
const { useDockerAutoUpdateState } =
  await import("./useDockerAutoUpdateState");

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={createTestQueryClient()}>
    {children}
  </QueryClientProvider>
);

describe("useDockerAutoUpdateState", () => {
  afterEach(() => vi.clearAllMocks());

  it("ignores an in-flight save after unmount and drops its queued follow-up", async () => {
    let resolveSave!: (state: typeof mocks.cacheState) => void;
    mocks.mutateAsync.mockImplementation(
      () => new Promise((resolve) => (resolveSave = resolve)),
    );
    const { result, unmount } = renderHook(
      () => useDockerAutoUpdateState(),
      { wrapper },
    );
    await waitFor(() => expect(result.current.state).toBeDefined());

    const firstOptions = {
      ...mocks.cacheState.options,
      container_names: ["one"],
    };
    const secondOptions = {
      ...mocks.cacheState.options,
      container_names: ["two"],
    };
    act(() => result.current.saveOptions(firstOptions));
    act(() => result.current.saveOptions(secondOptions));
    expect(mocks.mutateAsync).toHaveBeenCalledTimes(1);

    const setsBeforeUnmount = mocks.queryClient.setQueryData.mock.calls.length;
    unmount();
    await act(async () => resolveSave(mocks.cacheState));

    expect(mocks.mutateAsync).toHaveBeenCalledTimes(1);
    expect(mocks.queryClient.setQueryData).toHaveBeenCalledTimes(
      setsBeforeUnmount,
    );
    expect(mocks.toastSuccess).not.toHaveBeenCalled();
    expect(mocks.toastError).not.toHaveBeenCalled();
  });
});
