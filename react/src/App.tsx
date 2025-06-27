import { CacheProvider } from "@emotion/react";
import { ThemeProvider as MuiThemeProvider } from "@mui/material/styles";
import React, { useMemo } from "react";
import { useRoutes } from "react-router-dom";
import { Toaster } from "sonner";

import { AuthProvider } from "./contexts/AuthContext";
import routes from "./routes";
import createTheme from "./theme";
import ReactQueryProvider from "./utils/ReactQueryProvider";

import { SidebarProvider } from "@/contexts/SidebarContext";
import useTheme from "@/hooks/useAppTheme";
import createEmotionCache from "@/utils/createEmotionCache";

const clientSideEmotionCache = createEmotionCache();

function App({ emotionCache = clientSideEmotionCache }) {
  const content = useRoutes(routes);
  const { theme: themeName, primaryColor } = useTheme();
  const theme = useMemo(
    () => createTheme(themeName, primaryColor),
    [themeName, primaryColor],
  );

  return (
    <CacheProvider value={emotionCache}>
      <MuiThemeProvider theme={theme}>
        <ReactQueryProvider>
          <AuthProvider>
            <SidebarProvider>{content}</SidebarProvider>
          </AuthProvider>
        </ReactQueryProvider>
        <Toaster richColors position="top-right" />
      </MuiThemeProvider>
    </CacheProvider>
  );
}

export default App;
