import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Suspense, useEffect } from "react";

import { closeStreamMux, initStreamMux, useStreamMux } from "@/api";
import { capabilitiesQueryKey } from "@/api/capabilities";
import { RoutedTabLayout } from "@/components/tabbar";
import { AuthContext } from "@/contexts/AuthContext";
import RsyncPage from "@/routes/_authenticated/shares/-components/RsyncPage";
import { SHARES_TABS } from "@/routes/_authenticated/shares/-components/sharesTabs";
import type { AuthContextType } from "@/types/auth";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});
const auth: AuthContextType = {
  isAuthenticated: true,
  isInitialized: true,
  method: "session",
  privileged: true,
  refreshCapabilities: async () => {
    throw new Error("not used in fixture");
  },
  sessionExpired: () => {},
  signIn: async () => {},
  signOut: async () => {},
  user: { id: "rsync-fixture", name: "Rsync Fixture" },
};
queryClient.setQueryData(capabilitiesQueryKey(auth.user!.id), {
  rsync_available: true,
});

export default function RsyncFixture() {
  const { isOpen } = useStreamMux();
  useEffect(() => {
    initStreamMux();
    return closeStreamMux;
  }, []);
  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <Suspense fallback={<div role="status">Loading rsync…</div>}>
          <main
            style={{
              height: "100dvh",
              padding: "var(--app-space-16)",
              boxSizing: "border-box",
            }}
          >
            <RoutedTabLayout tabs={SHARES_TABS}>
              {isOpen ? <RsyncPage /> : <div role="status">Connecting…</div>}
            </RoutedTabLayout>
          </main>
        </Suspense>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}
