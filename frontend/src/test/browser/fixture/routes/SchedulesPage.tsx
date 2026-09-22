import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect } from "react";

import { closeStreamMux, initStreamMux, linuxio } from "@/api";
import SchedulesTab from "@/routes/_authenticated/services/-components/SchedulesTab";

const schedule = {
  definition: {
    id: "nightly",
    enabled: true,
    options: {
      name: "Nightly",
      script: "echo nightly",
      user: "root",
      arguments: [],
      on_calendar: "daily",
      timezone: "UTC",
      persistent: false,
      timeout_seconds: 300,
      working_directory: "",
    },
  },
  unit_name: "linuxio-schedule-nightly.service",
  active_state: "active",
  result: "success",
  invocation_id: "0123456789abcdef0123456789abcdef",
  exit_code: 0,
  exit_status: 0,
  next_run_at: null,
  last_run_at: null,
  can_stop: true,
  error: null,
};

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});
queryClient.setQueryData(linuxio.schedules.list.queryKey, {
  available: true,
  error: null,
  schedules: [schedule],
});
queryClient.setQueryData(
  linuxio.schedules.get({ id: "nightly" }).queryKey,
  schedule,
);
queryClient.setQueryData(linuxio.accounts.list_users.queryKey, [
  { username: "root" },
  { username: "alice" },
]);

export default function SchedulesFixture() {
  useEffect(() => {
    initStreamMux();
    return closeStreamMux;
  }, []);
  return (
    <QueryClientProvider client={queryClient}>
      <main
        style={{
          display: "flex",
          height: "100dvh",
          padding: "var(--app-space-24)",
        }}
      >
        <SchedulesTab
          listQueryOptions={linuxio.schedules.list}
          selected="nightly"
          selectedQueryOptions={linuxio.schedules.get({ id: "nightly" })}
          onSelectedChange={() => {}}
        />
      </main>
    </QueryClientProvider>
  );
}
