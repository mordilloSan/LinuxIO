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
import { renderHook } from "@/test/render";

const apiMocks = vi.hoisted(() => ({
  getIconUriUseQuery: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        get_icon_uri: {
          useQuery: apiMocks.getIconUriUseQuery,
        },
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
    apiMocks.getIconUriUseQuery.mockReturnValue({
      data: { uri: "data:image/svg+xml;base64,abc" },
      error: null,
      isError: false,
      isLoading: false,
    });
  });

  it("enables icon lookup only when an identifier is present and enabled", () => {
    const { result } = renderHook(() => useDockerIcon("si:nginx"));

    expect(result.current.iconUri).toBe("data:image/svg+xml;base64,abc");
    expect(apiMocks.getIconUriUseQuery).toHaveBeenCalledWith(
      "si:nginx",
      expect.objectContaining({
        enabled: true,
        retry: 1,
      }),
    );
  });

  it("returns null and disables the query for missing or disabled identifiers", () => {
    const missing = renderHook(() => useDockerIcon(undefined));
    const disabled = renderHook(() => useDockerIcon("si:nginx", false));

    expect(missing.result.current.iconUri).toBeNull();
    expect(disabled.result.current.iconUri).toBe(
      "data:image/svg+xml;base64,abc",
    );
    expect(apiMocks.getIconUriUseQuery.mock.calls[0]).toEqual([
      "",
      expect.objectContaining({ enabled: false }),
    ]);
    expect(apiMocks.getIconUriUseQuery.mock.calls[1]).toEqual([
      "si:nginx",
      expect.objectContaining({ enabled: false }),
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
