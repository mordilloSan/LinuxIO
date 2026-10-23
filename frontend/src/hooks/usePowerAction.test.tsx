import { describe, expect, it, vi } from "vitest";

import { PowerActionContext } from "@/contexts/PowerActionContext";
import usePowerAction from "@/hooks/usePowerAction";
import { renderHook } from "@/test/render";

describe("usePowerAction", () => {
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
});
