import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { ConfigContext } from "@/contexts/ConfigContext";
import { useConfig, useConfigValue } from "@/hooks/useConfig";
import { act, renderHook } from "@/test/render";
import type { ConfigContextType, EffectiveAppConfig } from "@/types/config";

const config: EffectiveAppConfig = {
  appSettings: {
    chunkSizeMB: 1,
    hiddenCards: [],
    layoutOrders: { dashboard: ["overview"] },
    navigationMode: "sidebar",
    dockTileColors: "accent",
    dockAccentGradient: {
      startColor: "",
      endColor: "",
      rangeStart: 0,
      rangeEnd: 100,
    },
    dockerDashboardSections: {
      overview: true,
      monitoring: true,
      daemon: true,
      resources: true,
    },
    hardwareSections: {
      overview: true,
      hardware: true,
      sensors: true,
      systemInfo: true,
      gpu: true,
      pciDevices: true,
      memoryModules: true,
    },
    primaryColor: "#2196f3",
    showHiddenFiles: true,
    sidebarCollapsed: false,
    theme: "DARK",
    viewModes: {
      "services.list": "card",
    },
    viewModeDefault: "card",
    terminalFontSize: 16,
  },
  docker: {
    folders: ["/var/lib/linuxio/docker"],
    requireMountsForFolders: false,
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
  return function Wrapper({ children }: { children: ReactNode }) {
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
    expect(() => renderHook(() => useConfigValue("theme"))).toThrow(
      "useConfig must be used within ConfigProvider",
    );
  });

  it("returns the config context", () => {
    const { result } = renderHook(() => useConfig(), {
      wrapper: wrapper(),
    });

    expect(result.current.config.appSettings.theme).toBe("DARK");
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
