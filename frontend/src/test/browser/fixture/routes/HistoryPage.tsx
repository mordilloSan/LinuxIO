import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { type ConfigContextType, linuxio, type TaskHistoryEntry } from "@/api";
import { ConfigContext } from "@/contexts/ConfigContext";
import HistoryPage from "@/routes/_authenticated/history/-components/HistoryPage";

const entries: TaskHistoryEntry[] = Array.from({ length: 30 }, (_, index) => ({
  task: {
    id: `job-${index}`,
    type: "filebrowser.compress",
    state: "completed",
    created_at: "2026-11-10T09:59:00Z",
    started_at: "2026-11-10T10:00:00Z",
    finished_at: "2026-11-10T10:02:05Z",
    updated_at: "2026-11-10T10:02:05Z",
    owner: { username: "alice" },
    metadata: { label: `Archive ${index}` },
  },
}));

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: Infinity } },
});
queryClient.setQueryData([...linuxio.tasks.history({}).queryKey, "pages"], {
  pages: [{ entries, more: false }],
  pageParams: [undefined],
});

// The page stores when it was opened; the fixture has nowhere to save it.
const config: ConfigContextType = {
  isLoaded: true,
  setKey: () => {},
  updateConfig: () => {},
};

export default function HistoryFixture() {
  return (
    <QueryClientProvider client={queryClient}>
      <ConfigContext value={config}>
        <main style={{ padding: "var(--app-space-24)", height: "100dvh" }}>
          <HistoryPage onSelectedChange={() => {}} />
        </main>
      </ConfigContext>
    </QueryClientProvider>
  );
}
