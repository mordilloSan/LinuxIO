import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { emptyCapabilityState } from "@/api/capabilities";
import SidebarNavList from "@/components/sidebar/SidebarNavList";
import { AuthContext } from "@/contexts/AuthContext";
import { useSidebarItems } from "@/routing/useSidebarItems";
import {
  createAuthContextValue,
  createTestQueryClient,
  render,
  renderHook,
} from "@/test/render";

const linkProps = vi.hoisted(() => ({
  calls: [] as Array<{
    preload?: unknown;
    preloadDelay?: unknown;
    to?: unknown;
  }>,
}));

vi.mock("@tanstack/react-router", () => ({
  Link: (props: {
    children: ReactNode;
    preload?: unknown;
    preloadDelay?: unknown;
    to?: unknown;
  }) => {
    linkProps.calls.push(props);
    return <a>{props.children}</a>;
  },
}));
function wrapper(auth = createAuthContextValue()) {
  const queryClient = createTestQueryClient();
  return function Wrapper({ children }: { children: ReactNode }) {
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

  it("uses native intent for every enabled link without a per-link delay", () => {
    const { result } = renderHook(() => useSidebarItems(), {
      wrapper: wrapper(
        createAuthContextValue({
          ...emptyCapabilityState,
          dockerAvailable: true,
          libvirtAvailable: true,
          lmSensorsAvailable: true,
          privileged: true,
          wireguardAvailable: true,
        }),
      ),
    });

    linkProps.calls.length = 0;
    render(
      <>
        {result.current.map((item) => (
          <SidebarNavList key={item.href} {...item} />
        ))}
      </>,
    );

    expect(linkProps.calls.map(({ preload, to }) => ({ preload, to }))).toEqual(
      result.current.map(({ href }) => ({ preload: "intent", to: href })),
    );
    expect(linkProps.calls.every((props) => !("preloadDelay" in props))).toBe(
      true,
    );
  });
});
