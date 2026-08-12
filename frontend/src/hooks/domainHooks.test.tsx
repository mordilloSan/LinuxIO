import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { PowerActionContext } from "@/contexts/PowerActionContext";
import {
  UpdateContext,
  UpdateNavigationContext,
  type UpdateContextValue,
} from "@/contexts/UpdateContext";
import { useDockerIcon } from "@/hooks/useDockerIcon";
import {
  useLinuxIOUpdater,
  useUpdateCanNavigate,
} from "@/hooks/useLinuxIOUpdater";
import usePowerAction from "@/hooks/usePowerAction";
import { createTestQueryClient, renderHook } from "@/test/render";

const queryClient = createTestQueryClient();
const queryWrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);

const apiMocks = vi.hoisted(() => ({
  getIconUriQueryOptions: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        get_icon_uri: Object.assign(apiMocks.getIconUriQueryOptions, {
          route: actual.linuxio.docker.get_icon_uri.route,
        }),
      },
    },
  };
});

const updateValue: UpdateContextValue = {
  canNavigate: true,
  error: null,
  isUpdating: false,
  output: [],
  phase: "idle",
  progress: 0,
  resetUpdate: vi.fn(),
  startUpdate: vi.fn(),
  status: "",
  targetVersion: null,
  updateComplete: false,
  updateSuccess: false,
};

describe("useDockerIcon", () => {
  beforeEach(() => {
    queryClient.clear();
    apiMocks.getIconUriQueryOptions.mockImplementation(
      (request: { identifier: string }) => ({
        queryKey: ["test", "docker-icon", request.identifier],
        queryFn: () =>
          Promise.resolve({ uri: "data:image/svg+xml;base64,abc" }),
        initialData: { uri: "data:image/svg+xml;base64,abc" },
      }),
    );
  });

  it("enables icon lookup only when an identifier is present and enabled", () => {
    const { result } = renderHook(() => useDockerIcon("si:nginx"), {
      wrapper: queryWrapper,
    });

    expect(result.current.iconUri).toBe("data:image/svg+xml;base64,abc");
    expect(apiMocks.getIconUriQueryOptions).toHaveBeenCalledWith({
      identifier: "si:nginx",
    });
  });

  it("returns null and disables the query for missing or disabled identifiers", () => {
    const missing = renderHook(() => useDockerIcon(undefined), {
      wrapper: queryWrapper,
    });
    const disabled = renderHook(() => useDockerIcon("si:nginx", false), {
      wrapper: queryWrapper,
    });

    expect(missing.result.current.iconUri).toBeNull();
    expect(disabled.result.current.iconUri).toBe(
      "data:image/svg+xml;base64,abc",
    );
    expect(apiMocks.getIconUriQueryOptions.mock.calls[0]).toEqual([
      { identifier: "" },
    ]);
    expect(apiMocks.getIconUriQueryOptions.mock.calls[1]).toEqual([
      { identifier: "si:nginx" },
    ]);
  });
});

describe("domain context hooks", () => {
  it("returns power actions from context and throws outside the provider", () => {
    expect(() => renderHook(() => usePowerAction())).toThrow(
      "usePowerAction must be used within a PowerActionProvider",
    );

    const value = {
      triggerPowerOff: vi.fn(),
      triggerReboot: vi.fn(),
    };
    const { result } = renderHook(() => usePowerAction(), {
      wrapper: ({ children }) => (
        <PowerActionContext.Provider value={value}>
          {children}
        </PowerActionContext.Provider>
      ),
    });

    expect(result.current).toBe(value);
  });

  it("returns update state and navigation guard from their contexts", () => {
    expect(() => renderHook(() => useLinuxIOUpdater())).toThrow(
      "UpdateContext must be placed within UpdateProvider",
    );
    expect(() => renderHook(() => useUpdateCanNavigate())).toThrow(
      "UpdateContext must be placed within UpdateProvider",
    );

    const updater = renderHook(() => useLinuxIOUpdater(), {
      wrapper: ({ children }) => (
        <UpdateContext.Provider value={updateValue}>
          {children}
        </UpdateContext.Provider>
      ),
    });
    const canNavigate = renderHook(() => useUpdateCanNavigate(), {
      wrapper: ({ children }) => (
        <UpdateNavigationContext.Provider value={false}>
          {children}
        </UpdateNavigationContext.Provider>
      ),
    });

    expect(updater.result.current).toBe(updateValue);
    expect(canNavigate.result.current).toBe(false);
  });
});
