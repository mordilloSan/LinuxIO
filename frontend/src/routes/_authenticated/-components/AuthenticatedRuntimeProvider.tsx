import { type PropsWithChildren, useMemo } from "react";

import { ANONYMOUS_USER_ID } from "@/api/user-scope";
import { AppUpdateProvider } from "@/contexts/AppUpdateProvider";
import { ComposeProviders, withProps } from "@/contexts/composeProviders";
import { ConfigProvider } from "@/contexts/ConfigProvider";
import { HeaderActionSlotProvider } from "@/contexts/HeaderActionSlotProvider";
import { PowerActionProvider } from "@/contexts/PowerActionProvider";
import { ToastProvider } from "@/contexts/ToastProvider";
import { useConfigValue } from "@/hooks/useConfig";
import { BackgroundTasksProvider } from "@/tasks/BackgroundTasksProvider";
import buildAppTheme, { AppThemeProvider } from "@/theme";

import { SidebarProvider } from "./sidebar/SidebarProvider";

interface AuthenticatedRuntimeProviderProps extends PropsWithChildren {
  userId?: string;
}
function AuthedThemeShell({ children }: PropsWithChildren) {
  const [themeName] = useConfigValue("theme");
  const [primaryColorName] = useConfigValue("primaryColor");
  const [themeColors] = useConfigValue("themeColors");
  const appTheme = useMemo(
    () => buildAppTheme(themeName, primaryColorName, themeColors),
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
        withProps(ConfigProvider, { key: userId ?? ANONYMOUS_USER_ID }),
        BackgroundTasksProvider,
        AuthedThemeShell,
        PowerActionProvider,
        AppUpdateProvider,
        SidebarProvider,
        HeaderActionSlotProvider,
      ]}
    >
      {children}
    </ComposeProviders>
  );
}
