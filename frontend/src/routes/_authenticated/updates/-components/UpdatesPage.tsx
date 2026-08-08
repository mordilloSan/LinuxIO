import { Icon } from "@iconify/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { linuxio } from "@/api";
import { RoutedTabActions } from "@/components/tabbar";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
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
  const theme = useAppTheme();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const { data: rawUpdates, refetch } = useSuspenseQuery(
    linuxio.updates.get_updates_basic.queryOptions({
      refetchInterval: 50000,
    }),
  );
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
    linuxio.updates.refresh_cache.useAction({
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
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: theme.spacing(1),
      }}
    >
      <AppTooltip title={isRefreshingCache ? "Refreshing" : "Refresh Sources"}>
        <AppIconButton
          aria-label="Refresh Sources"
          disabled={packageOperationPending}
          onClick={() => refreshCache()}
          size="small"
        >
          {isRefreshingCache ? (
            <AppCircularProgress color="inherit" size={20} />
          ) : (
            <Icon height={20} icon="mdi:database-refresh" width={20} />
          )}
        </AppIconButton>
      </AppTooltip>
      <AppTooltip title="Update settings">
        <AppIconButton
          aria-label="Open update settings"
          onClick={() => setSettingsOpen(true)}
          size="small"
        >
          <Icon height={20} icon="mdi:cog" width={20} />
        </AppIconButton>
      </AppTooltip>
      {actionableUpdates.length > 0 ? (
        <AppTooltip title={`Update All (${actionableUpdates.length})`}>
          <AppIconButton
            aria-label={`Update All (${actionableUpdates.length})`}
            disabled={packageOperationPending}
            onClick={() => updateAll(actionableUpdates.map((u) => u.package_id))}
            size="small"
          >
            <Icon height={20} icon="mdi:refresh" width={20} />
          </AppIconButton>
        </AppTooltip>
      ) : null}
    </div>
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
