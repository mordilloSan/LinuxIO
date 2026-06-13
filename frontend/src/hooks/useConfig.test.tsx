import { describe, expect, it, vi } from "vitest";

import { ConfigContext } from "@/contexts/ConfigContext";
import { useConfig, useConfigReady, useConfigValue } from "@/hooks/useConfig";
import { act, renderHook } from "@/test/render";
import type { AppConfig, ConfigContextType } from "@/types/config";

const config: AppConfig = {
  appSettings: {
    chunkSizeMB: 1,
    containerOrder: [],
    dashboardOrder: ["overview"],
    hiddenCards: [],
    primaryColor: "#2196f3",
    showHiddenFiles: true,
    sidebarCollapsed: false,
    theme: "DARK",
    viewModes: {
      "services.list": "card",
    },
  },
  docker: {
    folders: ["/var/lib/linuxio/docker"],
    proxy: {
      baseDomain: "",
      caddyEnabled: false,
      tlsEmail: "",
    },
  },
  jobs: {
    archiveCompressionWorkers: 0,
    archiveExtractWorkers: 0,
    heavyArchiveConcurrency: 1,
    notificationMinIntervalMs: 1000,
    progressMinBytesMB: 16,
    progressMinIntervalMs: 250,
  },
};

function makeContext(
  overrides: Partial<ConfigContextType> = {},
): ConfigContextType {
  return {
    config,
    isLoaded: true,
    setKey: vi.fn(),
    updateConfig: vi.fn(),
    ...overrides,
  };
}

function wrapper(value = makeContext()) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>
    );
  };
}

describe("useConfig", () => {
  it("throws outside ConfigProvider", () => {
    expect(() => renderHook(() => useConfig())).toThrow(
      "useConfig must be used within ConfigProvider",
    );
    expect(() => renderHook(() => useConfigReady())).toThrow(
      "useConfig must be used within ConfigProvider",
    );
    expect(() => renderHook(() => useConfigValue("theme"))).toThrow(
      "useConfig must be used within ConfigProvider",
    );
  });

  it("returns config context and ready state", () => {
    const value = makeContext({ isLoaded: false });

    const configHook = renderHook(() => useConfig(), {
      wrapper: wrapper(value),
    });
    const readyHook = renderHook(() => useConfigReady(), {
      wrapper: wrapper(value),
    });

    expect(configHook.result.current.config.appSettings.theme).toBe("DARK");
    expect(readyHook.result.current).toBe(false);
  });

  it("reads typed config values", () => {
    const { result } = renderHook(() => useConfigValue("showHiddenFiles"), {
      wrapper: wrapper(),
    });

    expect(result.current[0]).toBe(true);
  });

  it("passes direct config value updates to setKey", () => {
    const setKey = vi.fn();
    const { result } = renderHook(() => useConfigValue("theme"), {
      wrapper: wrapper(makeContext({ setKey })),
    });

    act(() => result.current[1]("LIGHT"));

    expect(setKey).toHaveBeenCalledWith("theme", "LIGHT");
  });

  it("passes functional config value updates to setKey", () => {
    const setKey = vi.fn();
    const { result } = renderHook(() => useConfigValue("sidebarCollapsed"), {
      wrapper: wrapper(makeContext({ setKey })),
    });

    act(() => result.current[1]((prev) => !prev));

    expect(setKey).toHaveBeenCalledTimes(1);
    const updater = setKey.mock.calls[0][1] as (prev: boolean) => boolean;
    expect(setKey.mock.calls[0][0]).toBe("sidebarCollapsed");
    expect(updater(false)).toBe(true);
  });
});
