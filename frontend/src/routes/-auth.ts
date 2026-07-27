import type { QueryClient } from "@tanstack/react-query";
import {
  notFound,
  type ParsedLocation,
  redirect,
} from "@tanstack/react-router";

import {
  hasAccessPolicy,
  type AccessContext,
  type AccessPolicy,
} from "@/hooks/useCapabilities";
import type { AuthState } from "@/types/auth";

export type RouterAuthSnapshot = Omit<
  Pick<AuthState, "isAuthenticated" | "isInitialized" | "user">,
  "isInitialized"
> & {
  isInitialized: true;
};

export interface LinuxIORouterContext {
  access: AccessContext;
  auth: RouterAuthSnapshot;
  isUpdateBlocked: () => boolean;
  queryClient: QueryClient;
}

type RouteLocation = Pick<ParsedLocation, "href"> & {
  search: Record<string, unknown>;
};

const INTERNAL_REDIRECT_BASE = "https://linuxio.invalid";

export function sanitizeInternalRedirect(value: unknown): string | undefined {
  if (
    typeof value !== "string" ||
    !value.startsWith("/") ||
    value.startsWith("//")
  ) {
    return undefined;
  }

  try {
    const url = new URL(value, INTERNAL_REDIRECT_BASE);
    if (url.origin !== INTERNAL_REDIRECT_BASE) return undefined;
    return `${url.pathname}${url.search}${url.hash}`;
  } catch {
    return undefined;
  }
}

export function requireAuthentication(
  context: LinuxIORouterContext,
  location: RouteLocation,
): void {
  if (context.auth.isAuthenticated) return;

  const redirectTarget =
    sanitizeInternalRedirect(location.search.redirect) ??
    sanitizeInternalRedirect(location.href) ??
    "/";

  throw redirect({
    replace: true,
    search: { redirect: redirectTarget },
    to: "/sign-in",
  });
}

export function requireGuest(
  context: LinuxIORouterContext,
  search: Record<string, unknown>,
): void {
  if (!context.auth.isAuthenticated) return;

  const redirectTarget = sanitizeInternalRedirect(search.redirect) ?? "/";

  throw redirect({ href: redirectTarget, replace: true });
}

export function requireAccess(
  policy: AccessPolicy,
  context: LinuxIORouterContext,
): void {
  if (hasAccessPolicy(policy, context.access)) return;
  throw notFound();
}
