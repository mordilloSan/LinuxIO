import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { linuxio, useCallMutation } from "@/api";
import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { partitionUpdatesByAvailability } from "@/utils/packageUpdates";

import { usePackageUpdateController } from "./PackageUpdateController";
import UpdateSettingsDialog from "./UpdateSettingsDialog";
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
  const [settingsOpen, setSettingsOpen] = useState(false);
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
    error,
    clearError,
    isUpdating,
    canCancel,
    recoveryPending,
  } = usePackageUpdateController();
  const { mutate: refreshCache, isPending: isRefreshingCache } =
    useCallMutation(linuxio.updates.refresh_cache, {
      success: async () => {
        await refetch();
        toast.success("Update sources refreshed");
      },
      error: "Failed to refresh update sources",
      toast: UPDATES_TOAST_META,
    });
  const packageOperationPending =
    recoveryPending || isUpdating || isRefreshingCache;
  const actions = (
    <>
      <AppActionIconButton
        ariaLabel="Refresh Sources"
        disabled={packageOperationPending}
        icon="mdi:database-refresh"
        iconSize={20}
        label={isRefreshingCache ? "Refreshing" : "Refresh Sources"}
        loading={isRefreshingCache}
        onClick={() => refreshCache()}
      />
      <AppActionIconButton
        ariaLabel="Open update settings"
        icon="mdi:cog"
        iconSize={20}
        label="Update settings"
        onClick={() => setSettingsOpen(true)}
      />
      {actionableUpdates.length > 0 ? (
        <AppActionIconButton
          ariaLabel={`Update All (${actionableUpdates.length})`}
          disabled={packageOperationPending}
          icon="mdi:refresh"
          iconSize={20}
          label={`Update All (${actionableUpdates.length})`}
          onClick={() => updateAll(actionableUpdates.map((u) => u.package_id))}
        />
      ) : null}
    </>
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <UpdateStatus
        error={error}
        eventLog={eventLog}
        canCancel={canCancel}
        isUpdating={isUpdating}
        onCancel={cancelUpdate}
        onClearError={clearError}
        onUpdateOne={updateOne}
        progress={progress}
        recoveryPending={recoveryPending}
        status={status}
        updates={updates}
        updatingPackage={updatingPackage}
      />

      <UpdateSettingsDialog
        onClose={() => setSettingsOpen(false)}
        open={settingsOpen}
      />
    </>
  );
};

export default UpdatesPage;
