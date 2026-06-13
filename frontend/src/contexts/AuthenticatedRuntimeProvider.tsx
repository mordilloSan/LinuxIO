import { type PropsWithChildren, useMemo } from "react";

import PageLoader from "@/components/loaders/PageLoader";
import { BackgroundJobsProvider } from "@/contexts/BackgroundJobsContext";
import { ConfigProvider } from "@/contexts/ConfigContext";
import { PowerActionProvider } from "@/contexts/PowerActionContext";
import { SidebarProvider } from "@/contexts/SidebarContext";
import { ToastProvider } from "@/contexts/ToastContext";
import { UpdateProvider } from "@/contexts/UpdateContext";
import { useConfigReady, useConfigValue } from "@/hooks/useConfig";
import { AppThemeProvider } from "@/theme";
import buildAppTheme from "@/theme";

interface AuthenticatedRuntimeProviderProps extends PropsWithChildren {
  userId?: string;
}

function AuthedThemeShell({ children }: PropsWithChildren) {
  const [themeName] = useConfigValue("theme");
  const [primaryColorName] = useConfigValue("primaryColor");
  const [themeColors] = useConfigValue("themeColors");
  const isLoaded = useConfigReady();

  const appTheme = useMemo(
    () =>
      buildAppTheme(
        String(themeName),
        primaryColorName as string | undefined,
        themeColors,
      ),
    [themeName, primaryColorName, themeColors],
  );

  if (!isLoaded) return <PageLoader />;
  return <AppThemeProvider value={appTheme}>{children}</AppThemeProvider>;
}

export default function AuthenticatedRuntimeProvider({
  children,
  userId,
}: AuthenticatedRuntimeProviderProps) {
  return (
    <ToastProvider>
      <ConfigProvider key={userId ?? "anonymous"}>
        <BackgroundJobsProvider>
          <AuthedThemeShell>
            <PowerActionProvider>
              <UpdateProvider>
                <SidebarProvider>{children}</SidebarProvider>
              </UpdateProvider>
            </PowerActionProvider>
          </AuthedThemeShell>
        </BackgroundJobsProvider>
      </ConfigProvider>
    </ToastProvider>
  );
}
