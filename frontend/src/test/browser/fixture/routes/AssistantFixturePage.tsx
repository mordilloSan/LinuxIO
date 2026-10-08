import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect } from "react";

import { closeStreamMux, initStreamMux } from "@/api";
import AssistantPage from "@/routes/_authenticated/assistant/-components/AssistantPage";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

export default function AssistantFixture() {
  useEffect(() => {
    initStreamMux();
    return closeStreamMux;
  }, []);
  return (
    <QueryClientProvider client={queryClient}>
      <main style={{ padding: "var(--app-space-24)", height: "100dvh" }}>
        <AssistantPage agent="claude" />
      </main>
    </QueryClientProvider>
  );
}
