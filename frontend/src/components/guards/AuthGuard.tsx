import { ThemeProvider as MuiThemeProvider } from "@mui/material/styles";
import React, { PropsWithChildren, useMemo } from "react";
import { Navigate, Outlet, useLocation } from "react-router-dom";

import PageLoader from "@/components/loaders/PageLoader";
import { ConfigProvider } from "@/contexts/ConfigContext";
import { SidebarProvider } from "@/contexts/SidebarContext";
import { WebSocketProvider } from "@/contexts/WebSocketContext";
import useAuth from "@/hooks/useAuth";
import { useConfigValue, useConfigReady } from "@/hooks/useConfig";
import createTheme from "@/theme";

function AuthedThemeShell({ children }: PropsWithChildren) {
  const [themeName] = useConfigValue("theme");
  const [primaryColorName] = useConfigValue("primaryColor");
  const isLoaded = useConfigReady();

  const muiTheme = useMemo(
    () =>
      createTheme(String(themeName), primaryColorName as string | undefined),
    [themeName, primaryColorName],
  );

  if (!isLoaded) return <PageLoader />;
  return <MuiThemeProvider theme={muiTheme}>{children}</MuiThemeProvider>;
}

export const AuthGuard: React.FC<PropsWithChildren> = ({ children }) => {
  const { isAuthenticated, isInitialized } = useAuth();
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

    return <Navigate to={to} replace />;
  }

  // Authenticated:
  // Only now mount WS + Config + Theme + Sidebar and the children (or nested routes)
  return (
    <WebSocketProvider>
      <ConfigProvider>
        <AuthedThemeShell>
          <SidebarProvider>{children ?? <Outlet />}</SidebarProvider>
        </AuthedThemeShell>
      </ConfigProvider>
    </WebSocketProvider>
  );
};
