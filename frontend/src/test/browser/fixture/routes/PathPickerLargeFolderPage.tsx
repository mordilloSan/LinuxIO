import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio } from "@/api";
import PathPickerField from "@/components/pathpicker/PathPickerField";

// One folder with many entries, half folders and half files, answered from
// the cache so the page needs no bridge. `?entries=` sets how many.
const entryCount = Number(
  new URLSearchParams(window.location.search).get("entries") ?? 3000,
);

const names = (prefix: string, count: number) =>
  Array.from(
    { length: count },
    (_, index) => `${prefix}-${String(index).padStart(5, "0")}`,
  );

const folders = names("dir", Math.ceil(entryCount / 2));
const files = names("file", Math.floor(entryCount / 2));

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: Infinity },
  },
});

queryClient.setQueryData(
  linuxio.filebrowser.directory_children({ includeFiles: true, path: "/" })
    .queryKey,
  { files, folders },
);

function LargeFolderFixture() {
  // The last folder: far below what the tree shows when it opens.
  const [value, setValue] = useState(`/${folders.at(-1)}/`);

  return (
    <main style={{ maxWidth: 480, padding: "var(--app-space-16)" }}>
      <h1>Path picker large folder fixture</h1>
      <PathPickerField
        includeFiles
        label="Folder"
        onChange={setValue}
        value={value}
      />
    </main>
  );
}

export default function PathPickerLargeFolderPage() {
  return (
    <QueryClientProvider client={queryClient}>
      <LargeFolderFixture />
    </QueryClientProvider>
  );
}
