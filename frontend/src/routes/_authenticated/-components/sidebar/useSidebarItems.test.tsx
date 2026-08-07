import { QueryClientProvider } from "@tanstack/react-query";
import { RouterContextProvider } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { emptyCapabilityState } from "@/api/capabilities";
import { AuthContext } from "@/contexts/AuthContext";
import {
  createAuthContextValue,
  createTestQueryClient,
  render,
  renderHook,
} from "@/test/render";

import SidebarNavList from "./SidebarNavList";
import { useSidebarItems } from "./useSidebarItems";
import { router } from "../../../../router/router";

const linkProps = vi.hoisted(() => ({
  calls: [] as Array<{
    params?: unknown;
    preload?: unknown;
    preloadDelay?: unknown;
    to?: unknown;
  }>,
}));

vi.mock("@tanstack/react-router", async () => {
  const actual = await vi.importActual("@tanstack/react-router");
  return {
    ...actual,
    Link: (props: {
      children: ReactNode;
      params?: unknown;
      preload?: unknown;
      preloadDelay?: unknown;
      to?: unknown;
    }) => {
      linkProps.calls.push(props);
      return <a>{props.children}</a>;
    },
  };
});
function wrapper(auth = createAuthContextValue()) {
  const queryClient = createTestQueryClient();
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={auth}>
          <RouterContextProvider router={router}>
            {children}
          </RouterContextProvider>
        </AuthContext.Provider>
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

  it("inherits global intent preloading without per-link overrides", () => {
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
          <SidebarNavList key={item.to} {...item} />
        ))}
      </>,
    );

    expect(
      linkProps.calls.map(({ params, preload, to }) => ({
        params,
        preload,
        to,
      })),
    ).toEqual(
      result.current.map(({ params, to }) => ({
        params,
        preload: undefined,
        to,
      })),
    );
    expect(linkProps.calls.every((props) => !("preloadDelay" in props))).toBe(
      true,
    );
  });
});
