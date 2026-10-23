import { describe, expect, it, vi } from "vitest";

import {
  AppUpdateContext,
  type AppUpdateContextValue,
} from "@/contexts/AppUpdateContext";
import { publishAppUpdateInProgress } from "@/contexts/appUpdateInProgress";
import { useAppUpdateInProgress, useAppUpdater } from "@/hooks/useAppUpdater";
import { act, renderHook } from "@/test/render";

const updateValue: AppUpdateContextValue = {
  error: null,
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

describe("app update hooks", () => {
  it("returns update state from its context and throws outside the provider", () => {
    expect(() => renderHook(() => useAppUpdater())).toThrow(
      "AppUpdateContext must be placed within AppUpdateProvider",
    );

    const { result } = renderHook(() => useAppUpdater(), {
      wrapper: ({ children }) => (
        <AppUpdateContext.Provider value={updateValue}>
          {children}
        </AppUpdateContext.Provider>
      ),
    });

    expect(result.current).toBe(updateValue);
  });

  it("follows the published in-progress flag", () => {
    const { result } = renderHook(() => useAppUpdateInProgress());
    expect(result.current).toBe(false);

    let release!: () => void;
    act(() => {
      release = publishAppUpdateInProgress({}, true);
    });
    expect(result.current).toBe(true);

    act(() => release());
    expect(result.current).toBe(false);
  });
});
