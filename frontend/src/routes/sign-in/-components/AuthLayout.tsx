import type { ReactNode } from "react";

import buildAppTheme, { AppThemeProvider, useAppMediaQuery } from "@/theme";
import { up } from "@/theme/breakpoints";

interface AuthLayoutProps {
  children: ReactNode;
}

// No user config exists before sign-in, so the route renders the default
// dark theme; the signed-in shell replaces it once the config loads.
const AUTH_THEME = buildAppTheme("DARK");

const AuthLayout = ({ children }: AuthLayoutProps) => {
  return (
    <AppThemeProvider value={AUTH_THEME}>
      <AuthContent>{children}</AuthContent>
    </AppThemeProvider>
  );
};

const AuthContent = ({ children }: AuthLayoutProps) => {
  const isSmallUp = useAppMediaQuery(up("sm"));

  return (
    <div
      style={{
        minHeight: "100vh",
        width: "100%",
        display: "grid",
        placeItems: "center",
        position: "relative",
        overflowX: "hidden",
        overflowY: "auto",
        paddingLeft: isSmallUp ? "var(--app-space-16)" : "var(--app-space-8)",
        paddingRight: isSmallUp ? "var(--app-space-16)" : "var(--app-space-8)",
        paddingTop: isSmallUp ? "var(--app-space-32)" : "var(--app-space-24)",
        paddingBottom: isSmallUp
          ? "var(--app-space-32)"
          : "var(--app-space-24)",
        backgroundImage:
          "radial-gradient(900px 420px at 12% 8%, color-mix(in srgb, var(--app-palette-primary-main), transparent 75%), color-mix(in srgb, var(--app-palette-background-default), transparent 100%) 60%), radial-gradient(800px 360px at 90% 0%, color-mix(in srgb, var(--accent-soft), transparent 80%), color-mix(in srgb, var(--app-palette-background-default), transparent 100%) 60%), linear-gradient(160deg, color-mix(in srgb, var(--app-palette-background-default), transparent 8%) 0%, var(--app-palette-background-default) 45%, color-mix(in srgb, var(--app-palette-background-default), transparent 28%) 100%)",
      }}
    >
      <div
        aria-hidden
        style={{
          position: "absolute",
          inset: 0,
          overflow: "hidden",
          pointerEvents: "none",
        }}
      >
        <div
          style={{
            position: "absolute",
            top: -140,
            right: -180,
            width: 420,
            height: 420,
            borderRadius: "50%",
            background:
              "radial-gradient(circle at 30% 30%, color-mix(in srgb, var(--app-palette-primary-main), transparent 65%), color-mix(in srgb, var(--app-palette-primary-main), transparent 100%) 70%)",
            opacity: 0.9,
          }}
        />
        <div
          style={{
            position: "absolute",
            bottom: -180,
            left: -160,
            width: 460,
            height: 460,
            borderRadius: "50%",
            background:
              "radial-gradient(circle at 70% 40%, color-mix(in srgb, var(--app-palette-primary-dark), transparent 70%), color-mix(in srgb, var(--app-palette-primary-dark), transparent 100%) 70%)",
            opacity: 0.8,
          }}
        />
      </div>
      <div
        style={{
          position: "relative",
          zIndex: 1,
          width: "100%",
          display: "flex",
          justifyContent: "center",
        }}
      >
        {children}
      </div>
    </div>
  );
};

export default AuthLayout;
