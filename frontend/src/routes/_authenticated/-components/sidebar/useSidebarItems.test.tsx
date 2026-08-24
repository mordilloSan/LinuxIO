import { QueryClientProvider } from "@tanstack/react-query";
import { RouterContextProvider } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import type { CapabilityState } from "@/api/capabilities";
import { AuthContext } from "@/contexts/AuthContext";
import {
  createAuthContextValue,
  createTestQueryClient,
  render,
  renderHook,
  seedCapabilityCache,
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
function wrapper({
  capabilities = {},
  privileged = false,
}: {
  capabilities?: Partial<CapabilityState>;
  privileged?: boolean;
} = {}) {
  const auth = createAuthContextValue({ privileged });
  const queryClient = createTestQueryClient();
  seedCapabilityCache(queryClient, capabilities);
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <AuthContext.Provider value={auth}>
        <RouterContextProvider router={router}>
          {/* Innermost so the app router's own Wrap (the module query
              client) cannot shadow the seeded test client. */}
          <QueryClientProvider client={queryClient}>
            {children}
          </QueryClientProvider>
        </RouterContextProvider>
      </AuthContext.Provider>
    );
  };
}

describe("useSidebarItems", () => {
  it("filters capability-gated and privileged routes", () => {
    const { result } = renderHook(() => useSidebarItems(), {
      wrapper: wrapper({
        capabilities: {
          dockerAvailable: false,
          lmSensorsAvailable: false,
          wireguardAvailable: true,
        },
        privileged: false,
      }),
    });

    const titles = result.current.map((item) => item.title);
    expect(titles).toContain("Dashboard");
    expect(titles).not.toContain("Docker");
    expect(titles).not.toContain("Hardware");
    expect(titles).not.toContain("Wireguard");
  });

  it("keeps sidebar items in configured order when access allows them", () => {
    const { result } = renderHook(() => useSidebarItems(), {
      wrapper: wrapper({
        capabilities: {
          dockerAvailable: true,
          lmSensorsAvailable: true,
          wireguardAvailable: true,
        },
        privileged: true,
      }),
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
      "Settings",
    ]);
  });

  it("inherits global intent preloading without per-link overrides", () => {
    const { result } = renderHook(() => useSidebarItems(), {
      wrapper: wrapper({
        capabilities: {
          dockerAvailable: true,
          libvirtAvailable: true,
          lmSensorsAvailable: true,
          wireguardAvailable: true,
        },
        privileged: true,
      }),
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
