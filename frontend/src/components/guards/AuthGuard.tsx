// AuthGuard.tsx
import { ThemeProvider as MuiThemeProvider } from "@mui/material/styles";
import React, { PropsWithChildren, useMemo } from "react";
import { Navigate, Outlet, useLocation } from "react-router-dom";

import PageLoader from "@/components/loaders/PageLoader";
import { ConfigProvider } from "@/contexts/ConfigContext";
import { SidebarProvider } from "@/contexts/SidebarContext";
import useAuth from "@/hooks/useAuth";
import { useConfigValue, useConfigReady } from "@/hooks/useConfig";
import createTheme from "@/theme";

function AuthedThemeShell({ children }: PropsWithChildren) {
  const [themeName] = useConfigValue("theme");
  const [primaryColorName] = useConfigValue("primaryColor");
  const isLoaded = useConfigReady();

  // Build MUI theme from variant + color *name* (resolver is inside createTheme)
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

  if (!isInitialized) return <PageLoader />;

  const isOnSignIn = location.pathname === "/sign-in";
  if (!isAuthenticated) {
    if (isOnSignIn) return <Outlet />;

    const params = new URLSearchParams(location.search);
    const existing = params.get("redirect");
    const target =
      existing || `${location.pathname}${location.search}${location.hash}`;
    const to = `/sign-in${target ? `?redirect=${encodeURIComponent(target)}` : ""}`;

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
