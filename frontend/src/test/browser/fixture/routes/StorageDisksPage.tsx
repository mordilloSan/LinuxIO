import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { type ApiDisk, linuxio } from "@/api";
import {
  capabilitiesQueryKey,
  wireFromCapabilityState,
} from "@/api/capabilities";
import { ANONYMOUS_USER_ID } from "@/api/user-scope";
import { AuthContext, type AuthContextType } from "@/contexts/AuthContext";
import DiskOverview from "@/routes/_authenticated/storage/-components/disk-overview/DiskOverview";
import { storageInventory, storageLive } from "@/test/storageTopologyFixture";

import "@fontsource-variable/inter/wght.css";

const drives: ApiDisk[] = [
  {
    model: "Samsung SSD 990 PRO",
    name: "nvme0n1",
    ro: false,
    rotational: false,
    serial: "SAMPLE-NVME",
    sizeBytes: 1099511627776,
    type: "nvme",
  },
  {
    model: "WDC WD40EFRX",
    name: "sda",
    ro: false,
    rotational: true,
    serial: "SAMPLE-SDA",
    sizeBytes: 4398046511104,
    type: "sata",
  },
];
const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: Infinity } },
});
const capabilities = wireFromCapabilityState({
  monitoringAvailable: true,
  smartmontoolsAvailable: false,
});
const auth: AuthContextType = {
  isAuthenticated: true,
  isInitialized: true,
  method: "session",
  privileged: false,
  user: null,
  refreshCapabilities: async () => capabilities,
  sessionExpired: () => {},
  signIn: async () => {},
  signOut: async () => {},
};
queryClient.setQueryData(capabilitiesQueryKey(ANONYMOUS_USER_ID), capabilities);
linuxio.storage.get_drive_info.queryFn = async () => drives;
linuxio.storage.list_nfs_mounts.queryFn = async () => [];
linuxio.storage.btrfs_list.queryFn = async () => [];
linuxio.storage.get_topology.queryFn = async () => storageInventory;
linuxio.monitoring.get_live.queryFn = async () => storageLive();

export default function StorageDisksFixture() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <main style={{ padding: "var(--app-space-24)" }}>
          <DiskOverview />
        </main>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}
