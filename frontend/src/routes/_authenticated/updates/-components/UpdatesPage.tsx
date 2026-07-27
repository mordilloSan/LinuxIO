import { Icon } from "@iconify/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { linuxio } from "@/api";
import { RoutedTabContainer } from "@/components/tabbar";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useCapability } from "@/hooks/useCapabilities";
import { usePackageUpdater } from "@/hooks/usePackageUpdater";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";

import UpdateSettingsDialog from "./UpdateSettingsDialog";
import { UPDATES_TABS } from "./updatesTabs";
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
      <RoutedTabContainer
        containerStyle={{ paddingInline: 0 }}
        tabs={UPDATES_TABS}
      >
        <AppAlert severity="warning">
          <AppAlertTitle>PackageKit unavailable</AppAlertTitle>
          {packageKitReason}
        </AppAlert>
      </RoutedTabContainer>
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
  } = usePackageUpdater(refetch);
  const { mutate: refreshCache, isPending: isRefreshingCache } =
    linuxio.updates.refresh_cache.useJobAction({
      success: async () => {
        await refetch();
        toast.success("Update sources refreshed");
      },
      error: "Failed to refresh update sources",
      toast: UPDATES_TOAST_META,
    });
  const packageOperationPending = !!updatingPackage || isRefreshingCache;

  return (
    <>
      <RoutedTabContainer
        containerStyle={{ paddingInline: 0 }}
        rightContent={
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(1),
            }}
          >
            <AppButton
              disabled={packageOperationPending}
              onClick={() => refreshCache()}
              size="small"
              startIcon={
                isRefreshingCache ? (
                  <AppCircularProgress color="inherit" size={16} />
                ) : (
                  <Icon height={20} icon="mdi:database-refresh" width={20} />
                )
              }
              variant="outlined"
            >
              {isRefreshingCache ? "Refreshing" : "Refresh Sources"}
            </AppButton>
            <AppTooltip title="Update settings">
              <AppIconButton
                aria-label="Open update settings"
                onClick={() => setSettingsOpen(true)}
                size="small"
              >
                <Icon height={20} icon="mdi:cog" width={20} />
              </AppIconButton>
            </AppTooltip>
            {updates.length > 0 ? (
              <AppButton
                disabled={packageOperationPending}
                onClick={() => updateAll(updates.map((u) => u.package_id))}
                size="small"
                startIcon={<Icon height={20} icon="mdi:refresh" width={20} />}
                variant="contained"
              >
                Update All ({updates.length})
              </AppButton>
            ) : null}
          </div>
        }
        tabs={UPDATES_TABS}
      >
        <UpdateStatus
          error={error}
          eventLog={eventLog}
          onCancel={cancelUpdate}
          onClearError={clearError}
          onUpdateOne={updateOne}
          progress={progress}
          status={status}
          updates={updates}
          updatingPackage={updatingPackage}
        />
      </RoutedTabContainer>

      <UpdateSettingsDialog
        onClose={() => setSettingsOpen(false)}
        open={settingsOpen}
      />
    </>
  );
};

export default UpdatesPage;
