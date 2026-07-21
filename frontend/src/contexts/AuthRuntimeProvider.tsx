import { type PropsWithChildren, useMemo } from "react";

import { BackgroundJobsProvider } from "@/contexts/BackgroundJobsContext";
import { ComposeProviders, withProps } from "@/contexts/composeProviders";
import { ConfigProvider } from "@/contexts/ConfigContext";
import { PowerActionProvider } from "@/contexts/PowerActionContext";
import { SidebarProvider } from "@/contexts/SidebarContext";
import { ToastProvider } from "@/contexts/ToastContext";
import { UpdateProvider } from "@/contexts/UpdateContext";
import { useConfigValue } from "@/hooks/useConfig";
import { AppThemeProvider } from "@/theme";
import buildAppTheme from "@/theme";

interface AuthenticatedRuntimeProviderProps extends PropsWithChildren {
  userId?: string;
}

function AuthedThemeShell({ children }: PropsWithChildren) {
  const [themeName] = useConfigValue("theme");
  const [primaryColorName] = useConfigValue("primaryColor");
  const [themeColors] = useConfigValue("themeColors");
  const appTheme = useMemo(
    () =>
      buildAppTheme(
        String(themeName),
        primaryColorName as string | undefined,
        themeColors,
      ),
    [themeName, primaryColorName, themeColors],
  );

  return <AppThemeProvider value={appTheme}>{children}</AppThemeProvider>;
}

export default function AuthenticatedRuntimeProvider({
  children,
  userId,
}: AuthenticatedRuntimeProviderProps) {
  return (
    <ComposeProviders
      providers={[
        ToastProvider,
        // key remounts config-scoped state (and everything below) when the
        // signed-in user changes, without dropping toast history above it.
        withProps(ConfigProvider, { key: userId ?? "anonymous" }),
        BackgroundJobsProvider,
        AuthedThemeShell,
        PowerActionProvider,
        UpdateProvider,
        SidebarProvider,
      ]}
    >
      {children}
    </ComposeProviders>
  );
}
