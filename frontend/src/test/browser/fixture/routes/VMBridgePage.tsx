import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Suspense, useEffect } from "react";

import {
  closeStreamMux,
  initStreamMux,
  useStreamMux,
  wireFromCapabilityState,
} from "@/api";
import type { ConfigContextType } from "@/api";
import { capabilitiesQueryKey } from "@/api/capabilities";
import { uiConfigQueryKey } from "@/api/config-query";
import { AuthContext, type AuthContextType } from "@/contexts/AuthContext";
import { ConfigContext } from "@/contexts/ConfigContext";
import { Route } from "@/routes/_authenticated/vm/route";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});
const capabilities = wireFromCapabilityState({
  libvirtAvailable: true,
});
const auth: AuthContextType = {
  isAuthenticated: true,
  isInitialized: true,
  method: "session",
  privileged: true,
  refreshCapabilities: async () => capabilities,
  sessionExpired: () => {},
  signIn: async () => {},
  signOut: async () => {},
  user: { id: "browser-fixture", name: "Browser Fixture" },
};
const config: ConfigContextType = {
  isLoaded: true,
  setKey: () => {},
  updateConfig: () => {},
};
const VMLayout = Route.options.component!;

queryClient.setQueryData(capabilitiesQueryKey(auth.user!.id), capabilities);
queryClient.setQueryData(uiConfigQueryKey(auth.user!.id), { layoutOrders: {} });

export default function VMBridgePage() {
  const { isOpen } = useStreamMux();
  useEffect(() => {
    initStreamMux();
    return closeStreamMux;
  }, []);

  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <ConfigContext value={config}>
          <Suspense fallback={<div role="status">Loading VMs…</div>}>
            {isOpen ? <VMLayout /> : <div role="status">Connecting…</div>}
          </Suspense>
        </ConfigContext>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}
