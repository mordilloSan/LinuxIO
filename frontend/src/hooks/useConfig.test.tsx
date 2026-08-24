import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { ConfigContext } from "@/contexts/ConfigContext";
import {
  useConfig,
  useConfigValue,
  useDockerSettings,
  useViewModeDefault,
} from "@/hooks/useConfig";
import {
  act,
  createConfigContextValue,
  createTestQueryClient,
  renderHook,
  seedConfigCache,
} from "@/test/render";
import type { ConfigContextType, EffectiveAppSettings } from "@/types/config";

function wrapper(
  overrides: Partial<ConfigContextType> = {},
  appSettings: Partial<EffectiveAppSettings> = {},
) {
  const queryClient = createTestQueryClient();
  seedConfigCache(queryClient, {
    viewModes: { "services.list": "card", "docker.containers": "table" },
    layoutOrders: { dashboard: ["overview"] },
    ...appSettings,
  });
  const value = createConfigContextValue(overrides);

  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <ConfigContext.Provider value={value}>
          {children}
        </ConfigContext.Provider>
      </QueryClientProvider>
    );
  };
}

describe("useConfig", () => {
  it("throws outside ConfigProvider", () => {
    expect(() => renderHook(() => useConfig())).toThrow(
      "useConfig must be used within ConfigProvider",
    );
    expect(() => renderHook(() => useConfigValue("theme"))).toThrow(
      "useConfig must be used within ConfigProvider",
    );
  });

  it("returns the config actions", () => {
    const { result } = renderHook(() => useConfig(), {
      wrapper: wrapper(),
    });

    expect(result.current.isLoaded).toBe(true);
    expect(typeof result.current.setKey).toBe("function");
    expect(typeof result.current.updateConfig).toBe("function");
  });

  it("reads bridge-owned values from the bridge snapshot", () => {
    const { result } = renderHook(() => useConfigValue("showHiddenFiles"), {
      wrapper: wrapper(),
    });

    expect(result.current[0]).toBe(true);
  });

  it("reads UI values from the UI snapshot", () => {
    const { result } = renderHook(() => useConfigValue("theme"), {
      wrapper: wrapper(),
    });

    expect(result.current[0]).toBe("DARK");
  });

  it("prunes stored view modes that match the backend default", () => {
    const { result } = renderHook(() => useConfigValue("viewModes"), {
      wrapper: wrapper(),
    });

    expect(result.current[0]).toEqual({ "docker.containers": "table" });
  });

  it("exposes the read-only view-mode default", () => {
    const { result } = renderHook(() => useViewModeDefault(), {
      wrapper: wrapper(),
    });

    expect(result.current).toBe("card");
  });

  it("reads docker settings from the bridge snapshot", () => {
    const { result } = renderHook(() => useDockerSettings(), {
      wrapper: wrapper(),
    });

    expect(result.current.requireMountsForFolders).toBe(false);
  });

  it("passes direct config value updates to setKey", () => {
    const setKey = vi.fn();
    const { result } = renderHook(() => useConfigValue("theme"), {
      wrapper: wrapper({ setKey }),
    });

    act(() => result.current[1]("LIGHT"));

    expect(setKey).toHaveBeenCalledWith("theme", "LIGHT");
  });

  it("passes functional config value updates to setKey", () => {
    const setKey = vi.fn();
    const { result } = renderHook(() => useConfigValue("sidebarCollapsed"), {
      wrapper: wrapper({ setKey }),
    });

    act(() => result.current[1]((prev) => !prev));

    expect(setKey).toHaveBeenCalledTimes(1);
    const updater = setKey.mock.calls[0][1] as (prev: boolean) => boolean;
    expect(setKey.mock.calls[0][0]).toBe("sidebarCollapsed");
    expect(updater(false)).toBe(true);
  });
});
