import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect } from "react";

import { closeStreamMux, initStreamMux, linuxio } from "@/api";
import { uiConfigQueryKey } from "@/api/config-query";
import { ConfigContext } from "@/contexts/ConfigContext";
import { Route } from "@/routes/_authenticated/network/route";
import type { ConfigContextType } from "@/types/config";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});
queryClient.setQueryData(linuxio.network.get_network_info.queryKey, []);
queryClient.setQueryData(uiConfigQueryKey("anonymous"), { layoutOrders: {} });

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
      <ConfigContext value={config}>
        <NetworkLayout />
      </ConfigContext>
    </QueryClientProvider>
  );
}
