import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { RoutedTabContainer } from "@/components/tabbar";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import { useCapability } from "@/hooks/useCapabilities";
import { loadRouteQueries } from "@/routes/-loader";

import UpdateHistory from "./-components/UpdateHistory";
import { UPDATES_TABS } from "./-components/updatesTabs";

export const Route = createFileRoute("/_authenticated/updates/history")({
  loader: ({ context, preload }) => {
    if (context.access.packageKitAvailable !== true) return;
    return loadRouteQueries({ context, preload }, [
      linuxio.updates.get_update_history.queryOptions(),
    ]);
  },
  component: UpdateHistoryRoute,
});

function UpdateHistoryRoute() {
  const { status, reason } = useCapability("packageKitAvailable");

  return (
    <RoutedTabContainer
      containerStyle={{ paddingInline: 0 }}
      tabs={UPDATES_TABS}
    >
      {status === "unavailable" ? (
        <AppAlert severity="warning">
          <AppAlertTitle>PackageKit unavailable</AppAlertTitle>
          {reason}
        </AppAlert>
      ) : (
        <UpdateHistory />
      )}
    </RoutedTabContainer>
  );
}
