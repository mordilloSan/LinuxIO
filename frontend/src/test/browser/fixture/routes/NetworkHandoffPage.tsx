import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect } from "react";

import { closeStreamMux, initStreamMux, linuxio } from "@/api";
import type { ConfigContextType } from "@/api";
import { uiConfigQueryKey } from "@/api/config-query";
import { ANONYMOUS_USER_ID } from "@/api/user-scope";
import { AuthContext, type AuthContextType } from "@/contexts/AuthContext";
import { ConfigContext } from "@/contexts/ConfigContext";
import { Route } from "@/routes/_authenticated/network/index";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});
queryClient.setQueryData(linuxio.network.get_network_info.queryKey, []);
queryClient.setQueryData(uiConfigQueryKey(ANONYMOUS_USER_ID), {
  layoutOrders: {},
});

// The interface editor shows its controls, the bridge handoff among them,
// only to a sudo session.
const auth: AuthContextType = {
  isAuthenticated: true,
  isInitialized: true,
  method: "session",
  privileged: true,
  user: null,
  refreshCapabilities: async () => ({}),
  sessionExpired: () => {},
  signIn: async () => {},
  signOut: async () => {},
};
const config: ConfigContextType = {
  isLoaded: true,
  setKey: () => {},
  updateConfig: () => {},
};
const NetworkLayout = Route.options.component!;

export default function NetworkHandoffPage() {
  useEffect(() => {
    initStreamMux();
    return closeStreamMux;
  }, []);

  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <ConfigContext value={config}>
          <NetworkLayout />
        </ConfigContext>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}
