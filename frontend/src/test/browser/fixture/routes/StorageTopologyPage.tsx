import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useLocation, useRouter, type AnyRouter } from "@tanstack/react-router";

import { linuxio } from "@/api";
import {
  capabilitiesQueryKey,
  wireFromCapabilityState,
  type CapabilitiesResponse,
} from "@/api/capabilities";
import { AuthContext } from "@/contexts/AuthContext";
import StorageTopologyPage from "@/routes/_authenticated/storage/-components/StorageTopologyPage";
import {
  storageContainers,
  storageGroups,
  storageInventory,
  storageLive,
  storageVMs,
} from "@/test/storageTopologyFixture";
import type { AuthContextType } from "@/types/auth";

import { seedDockerIcons } from "../dockerIcons";

import "@fontsource-variable/inter/wght.css";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: Infinity } },
});
const capabilities = wireFromCapabilityState({
  monitoringAvailable: true,
  dockerAvailable: true,
  libvirtAvailable: true,
});
const auth: AuthContextType = {
  isAuthenticated: true,
  isInitialized: true,
  method: "session",
  privileged: false,
  user: null,
  refreshCapabilities: async () => capabilities as CapabilitiesResponse,
  sessionExpired: () => {},
  signIn: async () => {},
  signOut: async () => {},
};
queryClient.setQueryData(capabilitiesQueryKey("anonymous"), capabilities);
seedDockerIcons(queryClient);
linuxio.storage.get_topology.queryFn = async () => storageInventory;
linuxio.storage.list_vgs.queryFn = async () => storageGroups;
linuxio.docker.list_containers.queryFn = async () => storageContainers;
linuxio.virt.list.queryFn = async () => storageVMs;
linuxio.monitoring.get_live.queryFn = async () => storageLive();

export default function StorageTopologyFixture() {
  const { search } = useLocation();
  const router = useRouter<AnyRouter>();
  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <main style={{ padding: "var(--app-space-24)" }}>
          <StorageTopologyPage
            selection={{
              node: typeof search.node === "string" ? search.node : undefined,
            }}
            onSelectionChange={(selection) =>
              void router.navigate<AnyRouter, string>({
                search: selection,
                resetScroll: false,
              })
            }
          />
        </main>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}
