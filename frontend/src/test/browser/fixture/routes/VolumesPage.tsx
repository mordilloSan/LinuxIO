import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio, type DockerVolume } from "@/api";
import { uiConfigQueryKey } from "@/api/config-query";
import { RoutedTabActions, RoutedTabLayout } from "@/components/tabbar";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { ConfigContext } from "@/contexts/ConfigContext";
import { BackgroundTasksActionsContext } from "@/contexts/TasksActionsContext";
import VolumeList from "@/routes/_authenticated/docker/-components/VolumeList";

import "@fontsource-variable/inter/wght.css";

const volumes: DockerVolume[] = [
  {
    Name: "immich_model-cache",
    Driver: "local",
    Scope: "local",
    Mountpoint: "/var/lib/docker/volumes/immich_model-cache/_data",
    UsageData: { Size: 2147483648, RefCount: 2 },
  },
  {
    Name: "unifi_unifi-data",
    Driver: "local",
    Scope: "local",
    Mountpoint: "/var/lib/docker/volumes/unifi_unifi-data/_data",
    UsageData: { Size: 0, RefCount: 0 },
  },
  {
    Name: "unifi_unifi-var-lib-mongodb-with-a-long-project-name",
    Driver: "local",
    Scope: "local",
    Mountpoint:
      "/var/lib/docker/volumes/unifi_unifi-var-lib-mongodb-with-a-long-project-name/_data",
    UsageData: { Size: 134217728, RefCount: 1 },
  },
  { Name: "remote-data", Driver: "plugin", Scope: "global", Mountpoint: "" },
];
const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: Infinity } },
});
queryClient.setQueryData(linuxio.docker.list_volumes.queryKey, volumes);
queryClient.setQueryData(uiConfigQueryKey("anonymous"), { layoutOrders: {} });

const actions = {
  cancelCompression: () => {},
  cancelCopy: () => {},
  cancelDownload: () => {},
  cancelExtraction: () => {},
  cancelTask: () => {},
  cancelMove: () => {},
  cancelUpload: () => {},
  closeIndexerDialog: () => {},
  openIndexerDialog: () => {},
  startCompression: async () => {},
  startCopy: async () => {},
  startDownload: async () => {},
  startExtraction: async () => {},
  startIndexer: async () => {},
  startMove: async () => {},
  startUpload: async () => ({ uploaded: 0, failures: [] }),
};

export default function VolumesPage() {
  const [viewMode, setViewMode] = useState<"table" | "card">("table");
  return (
    <QueryClientProvider client={queryClient}>
      <ConfigContext
        value={{ isLoaded: true, setKey: () => {}, updateConfig: () => {} }}
      >
        <BackgroundTasksActionsContext value={actions}>
          <main
            style={{
              boxSizing: "border-box",
              display: "flex",
              flexDirection: "column",
              height: "100dvh",
              minWidth: 0,
              paddingInline: "var(--app-space-16)",
            }}
          >
            <RoutedTabLayout
              tabs={[{ label: "Volumes", to: "/docker/volumes" }]}
            >
              <RoutedTabActions>
                <ViewModeToggle
                  alternateMode="table"
                  viewMode={viewMode}
                  onViewModeChange={setViewMode}
                />
              </RoutedTabActions>
              <VolumeList viewMode={viewMode} />
            </RoutedTabLayout>
          </main>
        </BackgroundTasksActionsContext>
      </ConfigContext>
    </QueryClientProvider>
  );
}
