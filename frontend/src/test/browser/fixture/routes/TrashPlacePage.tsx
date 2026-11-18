import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio } from "@/api";
import { emptyCapabilityState } from "@/api/capabilities";
import { AuthContext, type AuthContextType } from "@/contexts/AuthContext";
import Breadcrumbs from "@/routes/_authenticated/filebrowser/-components/Breadcrumbs";
import TrashPlace from "@/routes/_authenticated/filebrowser/-components/TrashPlace";
import type { TrashJobs } from "@/routes/_authenticated/filebrowser/-hooks/useTrashJobs";
import {
  DEFAULT_TRASH_SORT,
  type TrashSort,
} from "@/routes/_authenticated/filebrowser/-trashEntries";
import type { ViewMode } from "@/routes/_authenticated/filebrowser/-types";

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

// The trashed folder's size, which its properties read.
queryClient.setQueryData(
  linuxio.filebrowser.trash_size({
    path: "/home/alice/.local/share/Trash/files/photos",
  }).queryKey,
  { fileCount: 3, folderCount: 1, size: 4096 },
);

function TrashFixture() {
  const [requests, setRequests] = useState<string[]>([]);
  // The page keeps these in the URL and beside the folder listing.
  const [sort, setSort] = useState<TrashSort>(DEFAULT_TRASH_SORT);
  const [viewMode, setViewMode] = useState<ViewMode>("list");
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
          onSortChange={setSort}
          onSwitchView={() =>
            setViewMode((previous) => (previous === "card" ? "list" : "card"))
          }
          onTrashItems={(paths) => record(`trash:${paths.join(",")}`)}
          sort={sort}
          viewMode={viewMode}
        />
      </div>
    </main>
  );
}

// The app renders the file browser under AuthProvider, which the cards read
// (FileCard asks whether the session may size folders).
const authValue: AuthContextType = {
  ...emptyCapabilityState,
  isAuthenticated: false,
  isInitialized: true,
  method: "session",
  privileged: false,
  refreshCapabilities: async () => ({}),
  sessionExpired: () => {},
  signIn: async () => {},
  signOut: async () => {},
  user: null,
};

export default function TrashPlacePage() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue}>
        <TrashFixture />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}
