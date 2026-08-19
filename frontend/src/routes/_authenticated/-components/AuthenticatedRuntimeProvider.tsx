import { type PropsWithChildren, useMemo } from "react";

import { BackgroundTasksProvider } from "@/contexts/BackgroundTasksContext";
import { ComposeProviders, withProps } from "@/contexts/composeProviders";
import { ConfigProvider } from "@/contexts/ConfigProvider";
import { HeaderActionSlotProvider } from "@/contexts/HeaderActionSlotContext";
import { PowerActionProvider } from "@/contexts/PowerActionProvider";
import { ToastProvider } from "@/contexts/ToastProvider";
import { UpdateProvider } from "@/contexts/UpdateProvider";
import { useConfigValue } from "@/hooks/useConfig";
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
        withProps(ConfigProvider, { key: userId ?? "anonymous" }),
        BackgroundTasksProvider,
        AuthedThemeShell,
        PowerActionProvider,
        UpdateProvider,
        SidebarProvider,
        HeaderActionSlotProvider,
      ]}
    >
      {children}
    </ComposeProviders>
  );
}
