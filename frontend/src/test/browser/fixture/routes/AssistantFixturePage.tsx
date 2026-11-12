import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect } from "react";

import { closeStreamMux, initStreamMux } from "@/api";
import type { ConfigContextType } from "@/api";
import {
  capabilitiesQueryKey,
  wireFromCapabilityState,
} from "@/api/capabilities";
import { uiConfigQueryKey } from "@/api/config-query";
import { ANONYMOUS_USER_ID } from "@/api/user-scope";
import { AuthContext, type AuthContextType } from "@/contexts/AuthContext";
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
// The page starts the agent only once what it needs is installed.
queryClient.setQueryData(
  capabilitiesQueryKey(ANONYMOUS_USER_ID),
  wireFromCapabilityState({
    claudeCodeAvailable: true,
    assistantClaudeAvailable: true,
  }),
);

const stubConfig = {
  isLoaded: true,
  setKey: () => {},
  updateConfig: () => {},
} as unknown as ConfigContextType;

// The welcome screen reads the host's alerts, which only a privileged session
// may list; this one is not, so it shows the standing prompts.
const stubAuth = { privileged: false } as unknown as AuthContextType;

export default function AssistantFixture() {
  useEffect(() => {
    initStreamMux();
    return closeStreamMux;
  }, []);
  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext value={stubAuth}>
        <ConfigContext value={stubConfig}>
          <main style={{ padding: "var(--app-space-24)", height: "100dvh" }}>
            <AssistantPage
              agent="claude"
              chatDefaults={{ model: "opus", effort: "high", mode: "plan" }}
            />
          </main>
        </ConfigContext>
      </AuthContext>
    </QueryClientProvider>
  );
}
