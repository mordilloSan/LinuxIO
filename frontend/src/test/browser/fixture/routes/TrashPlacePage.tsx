import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio } from "@/api";
import Breadcrumbs from "@/routes/_authenticated/filebrowser/-components/Breadcrumbs";
import TrashPlace from "@/routes/_authenticated/filebrowser/-components/TrashPlace";
import type { TrashJobs } from "@/routes/_authenticated/filebrowser/-hooks/useTrashJobs";

// The real Trash place over a seeded list. There is no bridge behind the
// fixture, so its jobs only write down what they were asked to do: the specs
// cover browsing, selection and confirmation, never the tasks.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: Infinity },
  },
});
queryClient.setQueryData(linuxio.filebrowser.trash_list.queryKey, {
  entries: [
    {
      confirmRestore: false,
      deletedAt: "2026-01-02T03:04:05Z",
      isDir: false,
      name: "report.pdf",
      originalPath: "/home/alice/docs/report.pdf",
      path: "/home/alice/.local/share/Trash/files/report.pdf",
      size: 2048,
    },
    {
      confirmRestore: false,
      deletedAt: "2026-01-01T10:00:00Z",
      isDir: true,
      name: "photos",
      originalPath: "/srv/media/photos",
      path: "/home/alice/.local/share/Trash/files/photos",
      size: 0,
    },
    {
      confirmRestore: false,
      deletedAt: "",
      isDir: false,
      name: "mystery.bin",
      originalPath: "",
      path: "/home/alice/.local/share/Trash/files/mystery.bin",
      size: 12,
    },
  ],
});

function TrashFixture() {
  const [requests, setRequests] = useState<string[]>([]);
  const record = (line: string) =>
    setRequests((previous) => [...previous, line]);
  const jobs: TrashJobs = {
    purge: (paths) => record(`purge:${paths.join(",")}`),
    restore: (request) => record(`restore:${JSON.stringify(request)}`),
    restoreInto: (items, destination) =>
      record(`restoreInto:${destination}:${JSON.stringify(items)}`),
  };

  return (
    <main
      style={{
        boxSizing: "border-box",
        display: "flex",
        flexDirection: "column",
        height: "100dvh",
        padding: 24,
      }}
    >
      <h1>Trash place fixture</h1>
      <output data-testid="trash-jobs" style={{ whiteSpace: "pre-wrap" }}>
        {requests.length > 0 ? requests.join("\n") : "none"}
      </output>
      <div style={{ flex: 1, minHeight: 0 }}>
        <TrashPlace
          breadcrumbs={
            <Breadcrumbs
              onNavigate={(path) => record(`open:${path}`)}
              onRestoreItems={jobs.restoreInto}
              path="/home/alice"
              place={{
                icon: "mdi:trash-can-outline",
                label: "Trash",
                onDropItems: (paths) => record(`trash:${paths.join(",")}`),
              }}
            />
          }
          folderPath="/home/alice"
          jobs={jobs}
          onLeave={() => record("leave")}
          onTrashItems={(paths) => record(`trash:${paths.join(",")}`)}
        />
      </div>
    </main>
  );
}

export default function TrashPlacePage() {
  return (
    <QueryClientProvider client={queryClient}>
      <TrashFixture />
    </QueryClientProvider>
  );
}
