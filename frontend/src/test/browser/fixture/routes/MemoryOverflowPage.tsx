import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { linuxio } from "@/api";
import { AuthContext, type AuthContextType } from "@/contexts/AuthContext";
import MemoryUsage from "@/routes/_authenticated/-dashboard/Memory";
import { topologyLive } from "@/test/dockerTopologyFixture";
import {
  foreignZramConfig,
  swapFileDevice,
  swapFileRow,
  swapStatus,
} from "@/test/memoryStatusFixture";

import "@fontsource-variable/inter/wght.css";

const GIB = 1024 ** 3;

// A desktop on its swap file, with zram-tools installed and an
// /etc/default/zramswap the admin wrote: choosing zram removes the file and
// replaces that configuration, so the dialog asks first.
const status = swapStatus({
  devices: [swapFileDevice()],
  foreign_zram_config: foreignZramConfig(),
  mode: "disk",
  recommended_mode: "zswap",
  swap_files: [swapFileRow()],
  swappiness: 60,
});

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: Infinity } },
});
// The dashboard's Swap row is the pencil only for a session with sudo.
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
const live = topologyLive();
linuxio.monitoring.get_live.queryFn = async () => ({
  ...live,
  memory: {
    ...live.memory,
    swap_free_bytes: 3 * GIB,
    swap_total_bytes: 4 * GIB,
  },
});
linuxio.memory.get_swap_status.queryFn = async () => status;

/** The dashboard's Memory card, whose Swap row opens the dialog. */
export default function MemoryOverflowFixture() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <main style={{ padding: "var(--app-space-24)", width: 420 }}>
          <MemoryUsage />
        </main>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}
