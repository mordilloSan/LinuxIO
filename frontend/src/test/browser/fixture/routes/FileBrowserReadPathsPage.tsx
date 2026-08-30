import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { linuxio } from "@/api";
import AppButton from "@/components/ui/AppButton";
import DirectoryTree from "@/components/ui/DirectoryTree";
import { useListingInvalidation } from "@/hooks/filebrowser/useListingInvalidation";

const directoryChildren = linuxio.filebrowser.directory_children({
  includeFiles: true,
  path: "/",
});
const listing = linuxio.filebrowser.list_directory({ path: "/" });
const textFile = linuxio.filebrowser.read_text({ path: "/note.txt" });

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: Infinity },
  },
});

queryClient.setQueryData(directoryChildren.queryKey, {
  files: ["note.txt"],
  folders: ["folder"],
});
queryClient.setQueryData(listing.queryKey, {
  files: [
    {
      canOpenAsText: true,
      isRegularFile: true,
      modified: "2026-01-01T00:00:00Z",
      name: "note.txt",
      size: 12,
      symlink: false,
    },
  ],
  folders: [
    { modified: "2026-01-01T00:00:00Z", name: "folder", symlink: false },
  ],
});
queryClient.setQueryData(textFile.queryKey, {
  canSave: true,
  content: "seeded note",
  version: "version-1",
});

function cacheState(queryKey: readonly unknown[]) {
  const state = queryClient.getQueryState(queryKey);
  if (!state) return "missing";
  return [
    `data=${state.data === undefined ? "missing" : "cached"}`,
    `status=${state.status}`,
    `fetch=${state.fetchStatus}`,
    `freshness=${state.isInvalidated ? "stale" : "fresh"}`,
    `updated=${state.dataUpdatedAt}`,
  ].join(";");
}

function CacheState({
  label,
  queryKey,
  testId,
}: {
  label: string;
  queryKey: readonly unknown[];
  testId: string;
}) {
  const [, setVersion] = useState(0);

  useEffect(
    () =>
      queryClient
        .getQueryCache()
        .subscribe(() => setVersion((version) => version + 1)),
    [],
  );

  return (
    <output data-testid={testId}>
      {label}: {cacheState(queryKey)}
    </output>
  );
}

function ReadPathsFixture() {
  const invalidateListing = useListingInvalidation("/");

  return (
    <main style={{ padding: "var(--app-space-16)" }}>
      <h1>File-browser read paths fixture</h1>
      <AppButton onClick={invalidateListing}>Invalidate listing</AppButton>
      <div style={{ display: "grid", gap: 8, marginTop: 16 }}>
        <CacheState
          label="directory_children"
          queryKey={directoryChildren.queryKey}
          testId="directory-children-cache"
        />
        <CacheState
          label="list_directory"
          queryKey={listing.queryKey}
          testId="list-directory-cache"
        />
        <CacheState
          label="read_text"
          queryKey={textFile.queryKey}
          testId="read-text-cache"
        />
      </div>
      <DirectoryTree includeFiles onSelect={() => {}} rootPath="/" />
    </main>
  );
}

export default function FileBrowserReadPathsPage() {
  return (
    <QueryClientProvider client={queryClient}>
      <ReadPathsFixture />
    </QueryClientProvider>
  );
}
