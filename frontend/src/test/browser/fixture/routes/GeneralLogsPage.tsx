import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect } from "react";

import { closeStreamMux, initStreamMux } from "@/api";
import GeneralLogsPage from "@/routes/_authenticated/logs/-components/GeneralLogsPage";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

export default function GeneralLogsFixture() {
  useEffect(() => {
    initStreamMux();
    return closeStreamMux;
  }, []);

  return (
    <QueryClientProvider client={queryClient}>
      <main style={{ padding: "var(--app-space-24)", height: "100dvh" }}>
        <GeneralLogsPage />
      </main>
    </QueryClientProvider>
  );
}
