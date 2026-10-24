import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio } from "@/api";
import AppButton from "@/components/ui/AppButton";
import TrashDialog from "@/routes/_authenticated/filebrowser/-components/TrashDialog";

// The real Trash dialog over a seeded listing: there is no bridge behind the
// fixture, so the specs cover browsing and confirmation, never the tasks.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: Infinity },
  },
});
queryClient.setQueryData(linuxio.filebrowser.trash_list.queryKey, {
  entries: [
    {
      deletedAt: "2026-01-02T03:04:05Z",
      isDir: false,
      name: "report.pdf",
      originalPath: "/home/alice/docs/report.pdf",
      path: "/home/alice/.local/share/Trash/files/report.pdf",
      size: 2048,
    },
    {
      deletedAt: "2026-01-01T10:00:00Z",
      isDir: true,
      name: "photos",
      originalPath: "/srv/media/photos",
      path: "/home/alice/.local/share/Trash/files/photos",
      size: 0,
    },
    {
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
  const [open, setOpen] = useState(false);
  return (
    <main style={{ padding: 24 }}>
      <h1>Trash dialog fixture</h1>
      <AppButton onClick={() => setOpen(true)}>Open Trash</AppButton>
      <TrashDialog onClose={() => setOpen(false)} open={open} />
    </main>
  );
}

export default function TrashDialogPage() {
  return (
    <QueryClientProvider client={queryClient}>
      <TrashFixture />
    </QueryClientProvider>
  );
}
