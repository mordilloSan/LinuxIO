// AuthGuard.tsx
import { ThemeProvider as MuiThemeProvider } from "@mui/material/styles";
import React, { PropsWithChildren, useMemo } from "react";
import { Navigate, Outlet, useLocation } from "react-router-dom";

import PageLoader from "@/components/loaders/PageLoader";
import { ThemeProvider } from "@/contexts/ThemeContext";
import useAppTheme from "@/hooks/useAppTheme";
import useAuth from "@/hooks/useAuth";
import createTheme from "@/theme";

// Small wrapper that lives under ThemeProvider so it can use the hook
function AuthedThemeShell({ children }: PropsWithChildren) {
  const { theme, primaryColor, isLoaded } = useAppTheme();
  const muiTheme = useMemo(
    () => createTheme(theme, primaryColor),
    [theme, primaryColor],
  );
  if (!isLoaded) return null;
  return <MuiThemeProvider theme={muiTheme}>{children}</MuiThemeProvider>;
}

export const AuthGuard: React.FC<PropsWithChildren> = ({ children }) => {
  const { isAuthenticated, isInitialized } = useAuth();
  const location = useLocation();

  if (!isInitialized) return <PageLoader />;

  const isOnSignIn = location.pathname === "/sign-in";
  if (!isAuthenticated) {
    // Already on the login page? Don't redirect again.
    if (isOnSignIn) return <Outlet />;

    // Reuse existing redirect if present; otherwise build one ONCE
    const params = new URLSearchParams(location.search);
    const existing = params.get("redirect");
    const target =
      existing || `${location.pathname}${location.search}${location.hash}`;
    const to = `/sign-in${target ? `?redirect=${encodeURIComponent(target)}` : ""}`;
    return <Navigate to={to} replace />;
  }

  // Authenticated → mount theme providers here only
  return (
    <ThemeProvider>
      <AuthedThemeShell>{children ?? <Outlet />}</AuthedThemeShell>
    </ThemeProvider>
  );
};
