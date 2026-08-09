import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import { useCapability } from "@/hooks/useCapabilities";
import { loadRouteQueries } from "@/routes/-loader";

import UpdateHistory from "./-components/UpdateHistory";

export const Route = createFileRoute("/_authenticated/updates/history")({
  loader: (loaderArgs) => {
    const { context } = loaderArgs;
    if (context.access.packageKitAvailable !== true) return;
    return loadRouteQueries(loaderArgs, [linuxio.updates.get_update_history]);
  },
  component: UpdateHistoryRoute,
});

function UpdateHistoryRoute() {
  const { status, reason } = useCapability("packageKitAvailable");

  return status === "unavailable" ? (
    <AppAlert severity="warning">
      <AppAlertTitle>PackageKit unavailable</AppAlertTitle>
      {reason}
    </AppAlert>
  ) : (
    <UpdateHistory />
  );
}
