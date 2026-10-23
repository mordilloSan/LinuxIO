import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useLocation, useRouter, type AnyRouter } from "@tanstack/react-router";

import { linuxio } from "@/api";
import {
  capabilitiesQueryKey,
  wireFromCapabilityState,
} from "@/api/capabilities";
import { ANONYMOUS_USER_ID } from "@/api/user-scope";
import { AuthContext, type AuthContextType } from "@/contexts/AuthContext";
import DockerTopologyPage from "@/routes/_authenticated/docker/-components/topology/DockerTopologyPage";
import {
  topologyLive,
  topologyContainers,
  topologyNetworks,
} from "@/test/dockerTopologyFixture";

import { seedDockerIcons } from "../dockerIcons";

import "@fontsource-variable/inter/wght.css";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: Infinity } },
});
const auth: AuthContextType = {
  isAuthenticated: true,
  isInitialized: true,
  method: "session",
  privileged: false,
  user: null,
  refreshCapabilities: async () =>
    wireFromCapabilityState({
      monitoringAvailable: true,
    }),
  sessionExpired: () => {},
  signIn: async () => {},
  signOut: async () => {},
};
queryClient.setQueryData(
  capabilitiesQueryKey(ANONYMOUS_USER_ID),
  wireFromCapabilityState({ monitoringAvailable: true }),
);
queryClient.setQueryData(
  linuxio.docker.list_containers.queryKey,
  topologyContainers,
);
queryClient.setQueryData(
  linuxio.docker.list_networks.queryKey,
  topologyNetworks,
);
linuxio.docker.list_containers.queryFn = async () => topologyContainers;
linuxio.docker.list_networks.queryFn = async () => topologyNetworks;
linuxio.monitoring.get_live.queryFn = async () => topologyLive();

seedDockerIcons(queryClient);

export default function DockerTopologyFixture() {
  const { search } = useLocation();
  // This fixture router has its own dark/light routes, outside the app's
  // registered route tree. Production selection uses Route.useNavigate().
  const router = useRouter<AnyRouter>();
  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <main style={{ padding: "var(--app-space-24)" }}>
          <DockerTopologyPage
            onSelectionChange={(selection) =>
              void router.navigate<AnyRouter, string>({
                search: selection,
                resetScroll: false,
              })
            }
            selection={{
              container:
                typeof search.container === "string"
                  ? search.container
                  : undefined,
              network:
                typeof search.network === "string" ? search.network : undefined,
            }}
          />
        </main>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}
