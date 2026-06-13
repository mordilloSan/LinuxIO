import React, { lazy, type PropsWithChildren, Suspense } from "react";
import { Navigate, Outlet, useLocation } from "react-router-dom";

import PageLoader from "@/components/loaders/PageLoader";
import useAuth from "@/hooks/useAuth";

const AuthenticatedRuntimeProvider = lazy(
  () => import("@/contexts/AuthRuntimeProvider"),
);

export const AuthGuard: React.FC<PropsWithChildren> = ({ children }) => {
  const { isAuthenticated, isInitialized, user } = useAuth();
  const location = useLocation();

  // Block everything until we know the auth state
  if (!isInitialized) return <PageLoader />;

  const isOnSignIn = location.pathname === "/sign-in";

  // Not authenticated:
  if (!isAuthenticated) {
    // If we're on /sign-in, render the auth route tree (no app mounts)
    if (isOnSignIn) return <Outlet />;

    // Otherwise, push to /sign-in with redirect back here after login
    const params = new URLSearchParams(location.search);
    const existing = params.get("redirect");
    const target =
      existing || `${location.pathname}${location.search}${location.hash}`;
    const to = `/sign-in${target ? `?redirect=${encodeURIComponent(target)}` : ""}`;

    return <Navigate replace to={to} />;
  }

  // Authenticated:
  // Only now mount Config + Theme + Sidebar and the children (or nested routes)
  return (
    <Suspense fallback={<PageLoader />}>
      <AuthenticatedRuntimeProvider userId={user?.id}>
        {children ?? <Outlet />}
      </AuthenticatedRuntimeProvider>
    </Suspense>
  );
};
