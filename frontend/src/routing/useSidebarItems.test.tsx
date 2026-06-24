import { QueryClientProvider } from "@tanstack/react-query";
import React from "react";
import { describe, expect, it } from "vitest";

import { emptyCapabilityState } from "@/api/capabilities";
import { AuthContext } from "@/contexts/AuthContext";
import { useSidebarItems } from "@/routing/useSidebarItems";
import {
  createAuthContextValue,
  createTestQueryClient,
  renderHook,
} from "@/test/render";

function wrapper(auth = createAuthContextValue()) {
  const queryClient = createTestQueryClient();
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={auth}>{children}</AuthContext.Provider>
      </QueryClientProvider>
    );
  };
}

describe("useSidebarItems", () => {
  it("filters capability-gated and privileged routes", () => {
    const { result } = renderHook(() => useSidebarItems(), {
      wrapper: wrapper(
        createAuthContextValue({
          ...emptyCapabilityState,
          dockerAvailable: false,
          lmSensorsAvailable: false,
          privileged: false,
          wireguardAvailable: true,
        }),
      ),
    });

    const titles = result.current.map((item) => item.title);
    expect(titles).toContain("Dashboard");
    expect(titles).not.toContain("Docker");
    expect(titles).not.toContain("Hardware");
    expect(titles).not.toContain("Wireguard");
  });

  it("keeps sidebar items in configured order when access allows them", () => {
    const { result } = renderHook(() => useSidebarItems(), {
      wrapper: wrapper(
        createAuthContextValue({
          ...emptyCapabilityState,
          dockerAvailable: true,
          lmSensorsAvailable: true,
          privileged: true,
          wireguardAvailable: true,
        }),
      ),
    });

    expect(result.current.map((item) => item.title)).toEqual([
      "Dashboard",
      "Network",
      "Updates",
      "Services",
      "Logs",
      "Storage",
      "Docker",
      "Accounts",
      "Shares",
      "Wireguard",
      "Hardware",
      "Navigator",
      "Terminal",
    ]);
  });
});
