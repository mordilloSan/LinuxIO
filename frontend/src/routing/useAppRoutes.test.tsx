import React from "react";
import { describe, expect, it } from "vitest";

import { emptyCapabilityState } from "@/api/capabilities";
import { AuthContext } from "@/contexts/AuthContext";
import { useAppRoutes } from "@/routing/useAppRoutes";
import { createAuthContextValue, renderHook } from "@/test/render";

function wrapper(auth = createAuthContextValue()) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <AuthContext.Provider value={auth}>{children}</AuthContext.Provider>;
  };
}

// Paths of the protected `/` branch's children, with the index route ("") and
// the trailing catch-all ("*") included.
function protectedChildPaths(routes: ReturnType<typeof useAppRoutes>) {
  const branch = routes.find((route) => route.path === "/");
  return (
    branch?.children?.map((child) =>
      "path" in child ? child.path : undefined,
    ) ?? []
  );
}

describe("useAppRoutes", () => {
  it("builds the protected and sign-in branches with a catch-all", () => {
    const { result } = renderHook(() => useAppRoutes(), {
      wrapper: wrapper(),
    });

    expect(result.current.map((route) => route.path)).toEqual([
      "/",
      "/sign-in",
    ]);

    const childPaths = protectedChildPaths(result.current);
    expect(childPaths).toContain(""); // dashboard index route
    expect(childPaths.at(-1)).toBe("*"); // Page404 fallback is always last

    const signIn = result.current.find((route) => route.path === "/sign-in");
    expect(signIn?.children?.[0]).toMatchObject({ index: true });
  });

  it("excludes capability-gated and privileged routes the user cannot access", () => {
    const { result } = renderHook(() => useAppRoutes(), {
      wrapper: wrapper(
        createAuthContextValue({
          ...emptyCapabilityState,
          dockerAvailable: false,
          lmSensorsAvailable: false,
          privileged: false,
          wireguardAvailable: true, // gated by privilege, so still excluded
        }),
      ),
    });

    const childPaths = protectedChildPaths(result.current);
    expect(childPaths).toContain("network"); // ungated route stays
    expect(childPaths).not.toContain("docker"); // requiredCapabilities
    expect(childPaths).not.toContain("hardware"); // requiredCapabilities
    expect(childPaths).not.toContain("wireguard"); // requiresPrivileged
  });

  it("includes capability-gated and privileged routes when access allows them", () => {
    const { result } = renderHook(() => useAppRoutes(), {
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

    const childPaths = protectedChildPaths(result.current);
    expect(childPaths).toContain("docker");
    expect(childPaths).toContain("hardware");
    expect(childPaths).toContain("vm");
    expect(childPaths).toContain("wireguard");
  });
});
