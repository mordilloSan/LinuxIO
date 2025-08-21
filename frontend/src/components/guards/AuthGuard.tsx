// AuthGuard.tsx
import { ThemeProvider as MuiThemeProvider } from "@mui/material/styles";
import React, { PropsWithChildren, useMemo } from "react";
import { Navigate, Outlet, useLocation } from "react-router-dom";

import PageLoader from "@/components/loaders/PageLoader";
import { ConfigProvider } from "@/contexts/ConfigContext";
import useConfig from "@/hooks/useConfig";
import useAuth from "@/hooks/useAuth";
import createTheme from "@/theme";
import { SidebarProvider } from "@/contexts/SidebarContext";

// Small wrapper that lives under ConfigProvider so it can read config
function AuthedThemeShell({ children }: PropsWithChildren) {
  const { config, isLoaded } = useConfig();
  const { theme, primaryColor } = config;
  const muiTheme = useMemo(
    () => createTheme(theme, primaryColor),
    [theme, primaryColor],
  );

  if (!isLoaded) {
    // Don’t render sidebar or app content until config is ready
    return <PageLoader />;
  }

  return <MuiThemeProvider theme={muiTheme}>{children}</MuiThemeProvider>;
}

export const AuthGuard: React.FC<PropsWithChildren> = ({ children }) => {
  const { isAuthenticated, isInitialized } = useAuth();
  const location = useLocation();

  if (!isInitialized) {
    return <PageLoader />;
  }

  const isOnSignIn = location.pathname === "/sign-in";
  if (!isAuthenticated) {
    if (isOnSignIn) return <Outlet />;

    const params = new URLSearchParams(location.search);
    const existing = params.get("redirect");
    const target =
      existing || `${location.pathname}${location.search}${location.hash}`;
    const to = `/sign-in${target ? `?redirect=${encodeURIComponent(target)}` : ""
      }`;

    return <Navigate to={to} replace />;
  }

  // Authenticated → load config, then theme, then sidebar
  return (
    <ConfigProvider>
      <AuthedThemeShell>
        <SidebarProvider>{children ?? <Outlet />}</SidebarProvider>
      </AuthedThemeShell>
    </ConfigProvider>
  );
};
