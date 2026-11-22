import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect } from "react";

import { closeStreamMux, initStreamMux } from "@/api";
import type { ConfigContextType } from "@/api";
import { uiConfigQueryKey } from "@/api/config-query";
import { ANONYMOUS_USER_ID } from "@/api/user-scope";
import { ConfigContext } from "@/contexts/ConfigContext";
import Terminal from "@/routes/_authenticated/terminal/-components/Terminal";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});
// The terminal reads terminalFontSize from the config snapshot in the query
// cache; there is no ConfigProvider here to load one.
queryClient.setQueryData(uiConfigQueryKey(ANONYMOUS_USER_ID), {
  terminalFontSize: 14,
});

const stubConfig = {
  isLoaded: true,
  setKey: () => {},
  updateConfig: () => {},
} as unknown as ConfigContextType;

export default function TerminalFixture() {
  // The terminal opens its shell once the transport is up.
  useEffect(() => {
    initStreamMux();
    return closeStreamMux;
  }, []);
  return (
    <QueryClientProvider client={queryClient}>
      <ConfigContext value={stubConfig}>
        <main style={{ padding: "var(--app-space-24)", height: "100dvh" }}>
          <Terminal />
        </main>
      </ConfigContext>
    </QueryClientProvider>
  );
}
