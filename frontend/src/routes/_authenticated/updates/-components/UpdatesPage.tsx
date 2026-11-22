import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { linuxio } from "@/api";
import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import HeaderActions from "@/components/ui/HeaderActions";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { partitionUpdatesByAvailability } from "@/routes/_authenticated/updates/-components/packageUpdates";

import { usePackageUpdateController } from "./PackageUpdateController";
import UpdateStatus from "./UpdateStatus";

const UPDATES_TOAST_META = {
  label: "Open updates",
  to: "/updates",
} as const;

const UpdatesPage = () => {
  const { status: packageKitStatus, reason: packageKitReason } = useCapability(
    "packageKitAvailable",
  );
  const packageKitUnavailable = packageKitStatus === "unavailable";

  if (packageKitUnavailable) {
    return (
      <AppAlert severity="warning">
        <AppAlertTitle>PackageKit unavailable</AppAlertTitle>
        {packageKitReason}
      </AppAlert>
    );
  }

  return <AvailableUpdatesPage />;
};

const AvailableUpdatesPage = () => {
  const { data: rawUpdates, refetch } = useSuspenseQuery({
    ...linuxio.updates.get_updates_basic,
    refetchInterval: 50000,
  });
  const toast = useScopedToast(UPDATES_TOAST_META);

  const updates = useMemo(() => rawUpdates || [], [rawUpdates]);
  const { actionable: actionableUpdates } = useMemo(
    () => partitionUpdatesByAvailability(updates),
    [updates],
  );
  const {
    updateOne,
    updateAll,
    cancelUpdate,
    updatingPackage,
    progress,
    status,
    eventLog,
    output,
    outputHistoryIncomplete,
    error,
    clearError,
    isUpdating,
    canCancel,
    recoveryPending,
  } = usePackageUpdateController();
  // The last refresh's answer is what the page shows of LinuxIO's own
  // repository; it goes when the next refresh starts.
  const {
    mutate: refreshCache,
    isPending: isRefreshingCache,
    data: lastRefresh,
  } = linuxio.updates.refresh_cache.useMutation({
    success: async (result) => {
      await refetch();
      if (result.own_repository_error) {
        toast.warning(
          "Update sources refreshed, but LinuxIO's repository could not be read",
        );
      } else {
        toast.success("Update sources refreshed");
      }
    },
    error: "Failed to refresh update sources",
    toast: UPDATES_TOAST_META,
  });
  const ownRepositoryError = lastRefresh?.own_repository_error;
  const packageOperationPending =
    recoveryPending || isUpdating || isRefreshingCache;
  const actions = (
    <HeaderActions
      bulk={
        actionableUpdates.length > 0 ? (
          <AppActionIconButton
            ariaLabel={`Update All (${actionableUpdates.length})`}
            disabled={packageOperationPending}
            icon="mdi:package-up"
            iconSize={20}
            label={`Update All (${actionableUpdates.length})`}
            onClick={() =>
              updateAll(actionableUpdates.map((u) => u.package_id))
            }
          />
        ) : null
      }
      refresh={
        <AppActionIconButton
          ariaLabel="Refresh Sources"
          disabled={packageOperationPending}
          icon="mdi:database-refresh"
          iconSize={20}
          label={isRefreshingCache ? "Refreshing" : "Refresh Sources"}
          loading={isRefreshingCache}
          onClick={() => refreshCache()}
        />
      }
    />
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      {ownRepositoryError && (
        <AppAlert severity="warning">
          <AppAlertTitle>
            {"LinuxIO's package repository could not be read"}
          </AppAlertTitle>
          No LinuxIO update is listed until it can be read. Refresh Sources
          checks it again. The package manager reported: {ownRepositoryError}
        </AppAlert>
      )}
      <UpdateStatus
        error={error}
        eventLog={eventLog}
        canCancel={canCancel}
        isUpdating={isUpdating}
        onCancel={cancelUpdate}
        onClearError={clearError}
        onUpdateOne={updateOne}
        output={output}
        outputHistoryIncomplete={outputHistoryIncomplete}
        progress={progress}
        recoveryPending={recoveryPending}
        status={status}
        updates={updates}
        updatingPackage={updatingPackage}
      />
    </>
  );
};

export default UpdatesPage;
