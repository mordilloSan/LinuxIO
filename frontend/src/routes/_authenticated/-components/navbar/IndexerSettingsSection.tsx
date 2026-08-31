import { Icon } from "@iconify/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";

import {
  CACHE_TTL_MS,
  type IndexerConfig,
  type IndexerConfigPatch,
  type IndexerDaemonStatus,
  linuxio,
  useCallMutation,
} from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
import { useBackgroundTaskActions } from "@/hooks/backgroundTasks/useBackgroundTaskActions";
import { useIsIndexing } from "@/hooks/backgroundTasks/useIsIndexing";
import { compactGoDuration, isGoDuration } from "@/utils/durations";
import { formatDate, formatFileSize } from "@/utils/formaters";

import "./indexer-settings-section.css";
import {
  SettingsGrid,
  SettingsSaveFooter,
  SettingsSectionShell,
  useSettingsDraft,
} from "./SettingsSectionForm";
import {
  SectionCard,
  StatusMetric,
  ToggleCard,
} from "./SettingsSectionPrimitives";

type DraftConfig = Omit<IndexerConfig, "exclude_paths"> & {
  exclude_paths: string;
};
type DraftErrors = Partial<Record<keyof DraftConfig, string>>;

const INDEXER_TIMER_UNIT = "linuxio-indexer-index.timer";

const toDraft = (config: IndexerConfig): DraftConfig => ({
  ...config,
  exclude_paths: config.exclude_paths.join("\n"),
  interval: compactGoDuration(config.interval),
});

const toPatchPayload = (patch: Partial<DraftConfig>): IndexerConfigPatch => {
  const payload: IndexerConfigPatch = {};
  if (patch.exclude_paths !== undefined) {
    payload.exclude_paths = [
      ...new Set(
        patch.exclude_paths
          .split("\n")
          .map((path) => path.trim())
          .filter(Boolean),
      ),
    ];
  }
  if (patch.include_network_mounts !== undefined) {
    payload.include_network_mounts = patch.include_network_mounts;
  }
  return payload;
};

const validateDraft = (draft: DraftConfig): DraftErrors => {
  const errors: DraftErrors = {};
  const invalidPath = draft.exclude_paths
    .split("\n")
    .map((path) => path.trim())
    .find((path) => path && (!path.startsWith("/") || path === "/"));
  if (invalidPath) {
    errors.exclude_paths = "Use absolute paths other than /, one per line.";
  }
  if (!draft.interval.trim()) {
    errors.interval = "Timer interval is required.";
  } else if (!isGoDuration(draft.interval)) {
    errors.interval = "Use a duration like 30m, 6h, or 0.";
  }
  return errors;
};

const formatCount = (value?: number | null) =>
  typeof value === "number" ? value.toLocaleString() : "Unknown";

const formatStatusLabel = (value?: string | null) =>
  (value || "unknown")
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");

const statusColor = (status?: IndexerDaemonStatus) => {
  if (status?.running || status?.status === "indexing") {
    return "var(--app-palette-info-main)";
  }
  if (status?.status === "idle" || status?.status === "ready") {
    return "var(--app-palette-success-main)";
  }
  if (status?.status === "error" || status?.status === "failed") {
    return "var(--app-palette-error-main)";
  }
  return "var(--app-palette-warning-main)";
};

const IndexerSettingsSection = () => {
  const queryClient = useQueryClient();
  const { startIndexer } = useBackgroundTaskActions();
  const isIndexing = useIsIndexing();
  const configQuery = useQuery({
    ...linuxio.indexer.get_config,
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });
  const statusQuery = useQuery({
    ...linuxio.indexer.get_status,
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });
  const timerQueryOptions = {
    ...linuxio.systemd.get_unit_info({ unitName: INDEXER_TIMER_UNIT }),
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  };
  const timerQuery = useQuery(timerQueryOptions);
  const setConfigMutation = useCallMutation(linuxio.indexer.set_config, {
    error: "Failed to save indexer settings",
  });
  const setTimerMutation = useCallMutation(linuxio.indexer.set_timer_interval, {
    error: "Failed to save indexer timer",
    invalidates: [timerQueryOptions.queryKey],
  });
  const savedDraft = useMemo(
    () => (configQuery.data ? toDraft(configQuery.data) : null),
    [configQuery.data],
  );
  const { draft, draftPatch, errors, setErrors, isDirty, patchKey, reset } =
    useSettingsDraft<DraftConfig, DraftErrors>(savedDraft);
  const busy = setConfigMutation.isPending || setTimerMutation.isPending;
  const refreshing =
    configQuery.isFetching || statusQuery.isFetching || timerQuery.isFetching;

  const updateDraft = <K extends keyof DraftConfig>(
    key: K,
    value: DraftConfig[K],
  ) => {
    patchKey(key, value);
    setErrors((previous) => ({ ...previous, [key]: undefined }));
  };

  const saveChanges = async () => {
    if (!configQuery.data || !draft) return;
    const nextErrors = validateDraft(draft);
    if (Object.values(nextErrors).some(Boolean)) {
      setErrors(nextErrors);
      return;
    }
    const configPatch = toPatchPayload(draftPatch);
    const hasConfigChanges = Object.keys(configPatch).length > 0;
    const hasTimerChange = draftPatch.interval !== undefined;
    if (!hasConfigChanges && !hasTimerChange) return;
    try {
      let nextConfig = configQuery.data;
      if (hasConfigChanges) {
        nextConfig = (await setConfigMutation.mutateAsync(configPatch)).config;
      }
      if (hasTimerChange) {
        const timer = await setTimerMutation.mutateAsync({
          interval: draft.interval.trim(),
        });
        nextConfig = { ...nextConfig, interval: timer.interval };
      }
      queryClient.setQueryData(linuxio.indexer.get_config.queryKey, nextConfig);
      reset();
    } catch {
      // The mutation owns the user-facing error toast and the draft stays dirty.
    }
  };

  const refresh = () => {
    void configQuery.refetch();
    void statusQuery.refetch();
    void timerQuery.refetch();
  };
  const handleFullIndex = () => {
    void startIndexer({ onComplete: () => void statusQuery.refetch() });
  };

  const configError = configQuery.error;
  const statusError = statusQuery.error;
  const timerError = timerQuery.error;
  const shellProps = {
    title: "Indexer",
    subtitle: "Filesystem search, folder sizes, and index storage.",
    refreshAriaLabel: "Refresh indexer settings",
    refreshing,
    onRefresh: refresh,
  };

  if (configError) {
    return (
      <SettingsSectionShell {...shellProps}>
        <AppAlert severity="error">
          <AppAlertTitle>Indexer settings unavailable</AppAlertTitle>
          {configError.message}
        </AppAlert>
      </SettingsSectionShell>
    );
  }
  if (!draft) {
    return (
      <SettingsSectionShell {...shellProps}>
        <div style={{ padding: "var(--app-space-12)" }}>
          <ComponentLoader />
        </div>
      </SettingsSectionShell>
    );
  }

  const daemonStatus = statusQuery.data;
  const isUninitialized = daemonStatus?.status === "uninitialized";
  return (
    <SettingsSectionShell {...shellProps}>
      <SectionCard
        icon="mdi:chart-box-outline"
        subtitle="Daemon state, indexed entries, and storage info"
        title="Indexer Status"
      >
        {statusError ? (
          <AppAlert severity="warning">
            <AppAlertTitle>Indexer service unavailable</AppAlertTitle>
            {statusError.message}
          </AppAlert>
        ) : daemonStatus ? (
          <>
            {isUninitialized ? (
              <AppAlert severity="info">
                <AppAlertTitle>Run the first full index</AppAlertTitle>
                The indexer has no completed generation yet. Use the full index
                action below to initialize it.
              </AppAlert>
            ) : null}
            <div
              className="indexer-status-grid"
              style={{
                display: "grid",
                columnGap: "var(--app-space-6)",
                rowGap: "var(--app-space-6)",
              }}
            >
              <StatusMetric
                detail={daemonStatus.active_path}
                label="State"
                statusColor={statusColor(daemonStatus)}
                value={formatStatusLabel(daemonStatus.status)}
              />
              <StatusMetric
                label="Files"
                value={formatCount(daemonStatus.num_files)}
              />
              <StatusMetric
                label="Folders"
                value={formatCount(daemonStatus.num_dirs)}
              />
              <StatusMetric
                label="Indexed size"
                value={formatFileSize(daemonStatus.total_size, 1)}
              />
              <StatusMetric
                label="Database"
                value={formatFileSize(daemonStatus.database_size, 1)}
              />
              <StatusMetric
                label="Last indexed"
                value={formatDate(daemonStatus.last_indexed)}
              />
            </div>
            {daemonStatus.warning ? (
              <div style={{ marginTop: "var(--app-space-6)" }}>
                <AppAlert severity="warning">{daemonStatus.warning}</AppAlert>
              </div>
            ) : null}
          </>
        ) : (
          <ComponentLoader />
        )}
      </SectionCard>

      <SectionCard
        icon="mdi:magnify-scan"
        subtitle="Operator exclusions and network mount policy"
        title="Scan Policy"
      >
        <SettingsGrid minColumnWidth={220}>
          <AppTooltip title="Absolute paths skipped during indexing, one per line">
            <AppTextField
              disabled={busy}
              error={Boolean(errors.exclude_paths)}
              fullWidth
              helperText={errors.exclude_paths ?? "One absolute path per line."}
              label="Excluded paths"
              multiline
              onChange={(event) =>
                updateDraft("exclude_paths", event.target.value)
              }
              rows={4}
              size="small"
              value={draft.exclude_paths}
            />
          </AppTooltip>
          <ToggleCard
            checked={draft.include_network_mounts}
            description="Allow NFS, SMB, and CIFS mounts"
            disabled={busy}
            label="Include network mounts"
            onChange={(checked) =>
              updateDraft("include_network_mounts", checked)
            }
          />
        </SettingsGrid>
      </SectionCard>

      <SectionCard
        icon="mdi:timer-cog-outline"
        subtitle={INDEXER_TIMER_UNIT}
        title="Auto-Index Timer"
      >
        <AppTextField
          disabled={busy}
          error={Boolean(errors.interval)}
          fullWidth
          helperText={errors.interval ?? "Use 0 to disable recurring scans."}
          label="Timer interval"
          onChange={(event) => updateDraft("interval", event.target.value)}
          size="small"
          value={draft.interval}
        />
        {timerError ? (
          <div style={{ marginTop: "var(--app-space-6)" }}>
            <AppAlert severity="warning">
              <AppAlertTitle>Timer unavailable</AppAlertTitle>
              {timerError.message}
            </AppAlert>
          </div>
        ) : null}
      </SectionCard>

      <AppButton
        disabled={busy || isIndexing}
        onClick={handleFullIndex}
        startIcon={<Icon height={18} icon="mdi:sync" width={18} />}
        variant="outlined"
      >
        {isIndexing ? "Indexing..." : "Run full index"}
      </AppButton>

      <SettingsSaveFooter
        busy={busy}
        isDirty={isDirty}
        onReset={reset}
        onSave={() => void saveChanges()}
        saveDisabled={Object.values(errors).some(Boolean)}
        saving={setConfigMutation.isPending || setTimerMutation.isPending}
      />
    </SettingsSectionShell>
  );
};

export default IndexerSettingsSection;
