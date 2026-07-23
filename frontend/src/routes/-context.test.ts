import { QueryClient } from "@tanstack/react-query";
import { isNotFound, isRedirect } from "@tanstack/react-router";
import { describe, expect, it } from "vitest";

import { emptyCapabilityState } from "@/api/capabilities";
import {
  type LinuxIORouterContext,
  requireAccess,
  requireAuthentication,
  requireGuest,
} from "@/routes/-context";

function context(
  overrides: Partial<LinuxIORouterContext> = {},
): LinuxIORouterContext {
  return {
    access: { ...emptyCapabilityState, privileged: false },
    auth: {
      isAuthenticated: false,
      isInitialized: true,
      user: null,
    },
    isUpdateBlocked: () => false,
    queryClient: new QueryClient(),
    ...overrides,
  };
}

function captureThrow(callback: () => void): unknown {
  try {
    callback();
  } catch (error) {
    return error;
  }
  throw new Error("Expected callback to throw");
}

describe("router context guards", () => {
  it("preserves the complete protected destination on sign-in redirect", () => {
    const result = captureThrow(() =>
      requireAuthentication(context(), {
        href: "/filebrowser/srv/data?tail=200#preview",
        search: { tail: 200 },
      }),
    );

    expect(isRedirect(result)).toBe(true);
    if (!isRedirect(result)) return;
    expect(result.options).toMatchObject({
      replace: true,
      search: {
        redirect: "/filebrowser/srv/data?tail=200#preview",
      },
      to: "/sign-in",
    });
  });

  it("honors an existing redirect when authentication completes", () => {
    const result = captureThrow(() =>
      requireGuest(
        context({
          auth: {
            isAuthenticated: true,
            isInitialized: true,
            user: { id: "root", name: "root" },
          },
        }),
        { redirect: "/storage?storageTab=lvm" },
      ),
    );

    expect(isRedirect(result)).toBe(true);
    if (!isRedirect(result)) return;
    expect(result.options).toMatchObject({
      href: "/storage?storageTab=lvm",
      replace: true,
    });
  });

  it("enforces capability and privilege policies before loading", () => {
    const denied = captureThrow(() =>
      requireAccess(
        {
          requiredCapabilities: ["wireguardAvailable"],
          requiresPrivileged: true,
        },
        context(),
      ),
    );
    expect(isNotFound(denied)).toBe(true);

    expect(() =>
      requireAccess(
        {
          requiredCapabilities: ["wireguardAvailable"],
          requiresPrivileged: true,
        },
        context({
          access: {
            ...emptyCapabilityState,
            privileged: true,
            wireguardAvailable: true,
          },
        }),
      ),
    ).not.toThrow();
  });
});
