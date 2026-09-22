import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { useEffect } from "react";

import { closeStreamMux, initStreamMux } from "@/api";
import GeneralLogsPage from "@/routes/_authenticated/logs/-components/GeneralLogsPage";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

export default function GeneralLogsFixture() {
  const { unit, invocationId } = useSearch({ strict: false });
  useEffect(() => {
    initStreamMux();
    return closeStreamMux;
  }, []);

  return (
    <QueryClientProvider client={queryClient}>
      <main style={{ padding: "var(--app-space-24)", height: "100dvh" }}>
        <GeneralLogsPage
          unit={unit}
          invocationId={invocationId}
          key={`${unit ?? ""}:${invocationId ?? ""}`}
        />
      </main>
    </QueryClientProvider>
  );
}
