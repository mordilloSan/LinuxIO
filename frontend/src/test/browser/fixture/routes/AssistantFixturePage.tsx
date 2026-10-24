import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect } from "react";

import { closeStreamMux, initStreamMux } from "@/api";
import type { ConfigContextType } from "@/api";
import { uiConfigQueryKey } from "@/api/config-query";
import { ANONYMOUS_USER_ID } from "@/api/user-scope";
import { ConfigContext } from "@/contexts/ConfigContext";
import AssistantPage from "@/routes/_authenticated/assistant/-components/AssistantPage";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});
// The font size controls follow terminalFontSize through useConfigValue,
// which reads the config snapshot from the query cache; there is no
// ConfigProvider here to load one, so seed the one key the page reads.
queryClient.setQueryData(uiConfigQueryKey(ANONYMOUS_USER_ID), {
  terminalFontSize: 14,
});

const stubConfig = {
  isLoaded: true,
  setKey: () => {},
  updateConfig: () => {},
} as unknown as ConfigContextType;

export default function AssistantFixture() {
  useEffect(() => {
    initStreamMux();
    return closeStreamMux;
  }, []);
  return (
    <QueryClientProvider client={queryClient}>
      <ConfigContext value={stubConfig}>
        <main style={{ padding: "var(--app-space-24)", height: "100dvh" }}>
          <AssistantPage
            agent="claude"
            chatDefaults={{ model: "opus", effort: "high", mode: "plan" }}
          />
        </main>
      </ConfigContext>
    </QueryClientProvider>
  );
}
