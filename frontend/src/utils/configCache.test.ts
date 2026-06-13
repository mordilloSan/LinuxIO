import { describe, expect, it, vi } from "vitest";

import {
  clearConfigCache,
  readConfigCache,
  writeConfigCache,
} from "@/utils/configCache";
import type { AppConfig } from "@/types/config";

const config = {
  appSettings: {
    primaryColor: "blue",
    theme: "dark",
  },
} as unknown as AppConfig;

describe("configCache", () => {
  it("writes and reads per-user config", () => {
    writeConfigCache("miguel", config);

    expect(readConfigCache("miguel")).toEqual(config);
    expect(readConfigCache(null)).toBeNull();
    expect(readConfigCache("other")).toBeNull();
  });

  it("ignores malformed, stale, and invalid entries", () => {
    sessionStorage.setItem("linuxio_config:bad-json", "{");
    sessionStorage.setItem(
      "linuxio_config:old",
      JSON.stringify({ version: 0, config }),
    );
    sessionStorage.setItem(
      "linuxio_config:no-config",
      JSON.stringify({ version: 1, config: null }),
    );

    expect(readConfigCache("bad-json")).toBeNull();
    expect(readConfigCache("old")).toBeNull();
    expect(readConfigCache("no-config")).toBeNull();
  });

  it("clears config cache keys and theme bootstrap", () => {
    writeConfigCache("miguel", config);
    sessionStorage.setItem("keep", "yes");
    localStorage.setItem("linuxio_theme_bootstrap", "{}");

    clearConfigCache();

    expect(sessionStorage.getItem("linuxio_config:miguel")).toBeNull();
    expect(sessionStorage.getItem("keep")).toBe("yes");
    expect(localStorage.getItem("linuxio_theme_bootstrap")).toBeNull();
  });

  it("treats storage failures as best effort", () => {
    const setItem = vi
      .spyOn(Storage.prototype, "setItem")
      .mockImplementation(() => {
        throw new Error("quota");
      });

    expect(() => writeConfigCache("miguel", config)).not.toThrow();
    setItem.mockRestore();
  });
});
