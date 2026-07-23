import { useMemo, type ReactNode } from "react";
import { toast } from "sonner";

import {
  CACHE_TTL_MS,
  type IndexerConfig,
  type IndexerDaemonStatus,
  type IndexerIntegrityCheck,
  linuxio,
  type UnitInfo,
} from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
import StatusDot from "@/components/ui/StatusDot";
import { useCapability } from "@/hooks/useCapabilities";
import { type AppTheme, useAppTheme } from "@/theme";
import { compactGoDuration, isGoDuration } from "@/utils/durations";
import { formatDate, formatFileSize } from "@/utils/formaters";

import { getFTSStatus } from "./indexerFts";
import {
  getIntegrityCheckOption,
  INDEXER_INTEGRITY_CHECK_OPTIONS,
} from "./indexerIntegrity";
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

type DraftConfig = Omit<
  IndexerConfig,
  "keep_indexes" | "db_max_open_conns" | "db_max_idle_conns"
> & {
  keep_indexes: string;
  db_max_open_conns: string;
  db_max_idle_conns: string;
};

type DraftKey = keyof DraftConfig;
type DraftErrors = Partial<Record<DraftKey, string>>;

const JOURNAL_MODES = ["WAL", "DELETE", "TRUNCATE", "PERSIST", "MEMORY", "OFF"];
const SYNCHRONOUS_MODES = ["OFF", "NORMAL", "FULL", "EXTRA"];
const AUTO_VACUUM_MODES = ["INCREMENTAL", "FULL", "NONE"];
const DB_MODE_LABELS: Record<string, string> = {
  DELETE: "Delete",
  EXTRA: "Extra",
  FULL: "Full",
  INCREMENTAL: "Incremental",
  MEMORY: "Memory",
  NONE: "None",
  NORMAL: "Normal",
  OFF: "Off",
  PERSIST: "Persist",
  TRUNCATE: "Truncate",
  WAL: "WAL",
};
const RESTART_FIELDS: DraftKey[] = [
  "db_path",
  "db_busy_timeout",
  "db_journal_mode",
  "db_synchronous",
  "db_auto_vacuum",
  "db_max_open_conns",
  "db_max_idle_conns",
  "db_conn_max_idle_time",
  "socket_path",
  "listen_addr",
];
const INDEXER_TIMER_UNIT = "indexer-index.timer";

const toDraft = (config: IndexerConfig): DraftConfig => ({
  ...config,
  integrity_check: config.integrity_check || "full",
  interval: compactGoDuration(config.interval),
  keep_indexes: String(config.keep_indexes),
  db_max_open_conns: String(config.db_max_open_conns),
  db_max_idle_conns: String(config.db_max_idle_conns),
});

const toPatchPayload = (
  patch: Partial<DraftConfig>,
): Partial<IndexerConfig> => {
  const payload: Partial<IndexerConfig> = {};

  if (patch.index_path !== undefined) {
    payload.index_path = patch.index_path.trim();
  }
  if (patch.index_name !== undefined) {
    payload.index_name = patch.index_name.trim();
  }
  if (patch.include_hidden !== undefined) {
    payload.include_hidden = patch.include_hidden;
  }
  if (patch.include_network_mounts !== undefined) {
    payload.include_network_mounts = patch.include_network_mounts;
  }
  if (patch.fresh_index !== undefined) {
    payload.fresh_index = patch.fresh_index;
  }
  if (patch.fts_search !== undefined) {
    payload.fts_search = patch.fts_search;
  }
  if (patch.keep_indexes !== undefined) {
    payload.keep_indexes = Number(patch.keep_indexes);
  }
  if (patch.integrity_check !== undefined) {
    payload.integrity_check = patch.integrity_check;
  }
  if (patch.db_path !== undefined) {
    payload.db_path = patch.db_path.trim();
  }
  if (patch.db_busy_timeout !== undefined) {
    payload.db_busy_timeout = patch.db_busy_timeout.trim();
  }
  if (patch.db_journal_mode !== undefined) {
    payload.db_journal_mode = patch.db_journal_mode.trim();
  }
  if (patch.db_synchronous !== undefined) {
    payload.db_synchronous = patch.db_synchronous.trim();
  }
  if (patch.db_auto_vacuum !== undefined) {
    payload.db_auto_vacuum = patch.db_auto_vacuum.trim();
  }
  if (patch.db_max_open_conns !== undefined) {
    payload.db_max_open_conns = Number(patch.db_max_open_conns);
  }
  if (patch.db_max_idle_conns !== undefined) {
    payload.db_max_idle_conns = Number(patch.db_max_idle_conns);
  }
  if (patch.db_conn_max_idle_time !== undefined) {
    payload.db_conn_max_idle_time = patch.db_conn_max_idle_time.trim();
  }
  if (patch.socket_path !== undefined) {
    payload.socket_path = patch.socket_path.trim();
  }
  if (patch.listen_addr !== undefined) {
    payload.listen_addr = patch.listen_addr.trim();
  }

  return payload;
};

const isAbsolutePath = (value: string) => value.trim().startsWith("/");

const isNonNegativeInteger = (value: string) => /^\d+$/.test(value.trim());

const validateDraft = (draft: DraftConfig): DraftErrors => {
  const errors: DraftErrors = {};

  if (!draft.index_path.trim()) {
    errors.index_path = "Index path is required.";
  } else if (!isAbsolutePath(draft.index_path)) {
    errors.index_path = "Index path must be absolute.";
  }

  if (!draft.index_name.trim()) {
    errors.index_name = "Index name is required.";
  }

  if (!isNonNegativeInteger(draft.keep_indexes)) {
    errors.keep_indexes = "Use a non-negative whole number.";
  }

  if (!draft.interval.trim()) {
    errors.interval = "Timer interval is required.";
  } else if (!isGoDuration(draft.interval)) {
    errors.interval = "Use a duration like 30m, 6h, or 0.";
  }

  if (!draft.db_path.trim()) {
    errors.db_path = "Database path is required.";
  } else if (!isAbsolutePath(draft.db_path)) {
    errors.db_path = "Database path must be absolute.";
  }

  if (!draft.db_busy_timeout.trim()) {
    errors.db_busy_timeout = "Busy timeout is required.";
  }

  if (!draft.db_conn_max_idle_time.trim()) {
    errors.db_conn_max_idle_time = "Idle time is required.";
  }

  if (!isNonNegativeInteger(draft.db_max_open_conns)) {
    errors.db_max_open_conns = "Use a non-negative whole number.";
  }

  if (!isNonNegativeInteger(draft.db_max_idle_conns)) {
    errors.db_max_idle_conns = "Use a non-negative whole number.";
  }

  if (!draft.socket_path.trim()) {
    errors.socket_path = "Socket path is required for LinuxIO.";
  } else if (!isAbsolutePath(draft.socket_path)) {
    errors.socket_path = "Socket path must be absolute.";
  }

  const listenAddr = draft.listen_addr.trim();
  if (listenAddr && !listenAddr.includes(":")) {
    errors.listen_addr = "Use host:port, :port, or leave empty.";
  }

  return errors;
};

const hasErrors = (errors: DraftErrors) => Object.values(errors).some(Boolean);

const formatCount = (value?: number | null) =>
  typeof value === "number" ? value.toLocaleString() : "Unknown";

const formatStatusLabel = (value?: string | null) => {
  if (!value) return "Unknown";
  return value
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
};

const formatActiveIndexerStatus = (status: IndexerDaemonStatus) =>
  formatStatusLabel(status.active_operation || status.status);

const formatTimerTimestamp = (usec: unknown, fallback: string): string => {
  const value = Number(usec ?? 0);
  if (!value || !Number.isFinite(value) || value >= Number.MAX_SAFE_INTEGER) {
    return fallback;
  }
  const date = new Date(value / 1000);
  return Number.isNaN(date.getTime()) ? fallback : date.toLocaleString();
};

const formatTimerState = (info: UnitInfo | undefined) => {
  const activeState = String(info?.ActiveState ?? "unknown");
  const subState = String(info?.SubState ?? "");
  const activeLabel = formatStatusLabel(activeState);
  const subLabel = formatStatusLabel(subState);
  return subState && subState !== activeState
    ? `${activeLabel} (${subLabel})`
    : activeLabel;
};

const getTimerColor = (info: UnitInfo | undefined, theme: AppTheme) => {
  const activeState = String(info?.ActiveState ?? "").toLowerCase();
  if (activeState === "active") {
    return theme.palette.success.main;
  }
  if (activeState === "activating") {
    return theme.palette.info.main;
  }
  if (activeState === "failed") {
    return theme.palette.error.main;
  }
  return theme.palette.warning.main;
};

const getStatusColor = (
  status: IndexerDaemonStatus | undefined,
  theme: AppTheme,
) => {
  const normalizedStatus = status?.status ?? "unknown";
  if (status?.running || normalizedStatus === "indexing") {
    return theme.palette.info.main;
  }
  if (normalizedStatus === "idle" || normalizedStatus === "ready") {
    return theme.palette.success.main;
  }
  if (normalizedStatus === "error" || normalizedStatus === "failed") {
    return theme.palette.error.main;
  }
  return theme.palette.warning.main;
};

const IndexerSettingsSection = () => {
  const theme = useAppTheme();
  const indexerConfigCache = linuxio.indexer.get_config.useCache();
  const {
    isEnabled: indexerEnabled,
    status: indexerStatus,
    reason: indexerReason,
  } = useCapability("indexerAvailable");

  const {
    data: config,
    isLoading,
    error,
    refetch,
    isFetching,
  } = linuxio.indexer.get_config.useQuery({
    enabled: indexerEnabled,
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });
  const {
    data: daemonStatus,
    error: statusError,
    refetch: refetchStatus,
    isFetching: isStatusFetching,
  } = linuxio.indexer.get_status.useQuery({
    enabled: indexerEnabled,
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });
  const {
    data: timerInfo,
    error: timerError,
    refetch: refetchTimer,
    isFetching: isTimerFetching,
  } = linuxio.systemd.get_unit_info.useQuery(INDEXER_TIMER_UNIT, {
    enabled: indexerEnabled,
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });

  const setConfigMutation = linuxio.indexer.set_config.useJobAction({
    error: "Failed to save indexer settings",
  });
  const setTimerMutation = linuxio.indexer.set_timer_interval.useJobAction({
    error: "Failed to save indexer timer",
    invalidates: [linuxio.systemd.get_unit_info.queryKey(INDEXER_TIMER_UNIT)],
  });

  const savedDraft = useMemo(() => (config ? toDraft(config) : null), [config]);
  const {
    draft,
    draftPatch,
    setDraftPatch,
    errors,
    setErrors,
    restartRequired,
    setRestartRequired,
    isDirty,
    patchKey,
    reset: handleReset,
  } = useSettingsDraft<DraftConfig, DraftErrors>(savedDraft);
  const busy = setConfigMutation.isPending || setTimerMutation.isPending;
  const refreshing = isFetching || isStatusFetching || isTimerFetching;
  const statusTooltip = formatStatusLabel(daemonStatus?.status);
  const timerTooltip = formatTimerState(timerInfo);
  const willRequireRestart = useMemo(() => {
    if (!draft || !savedDraft) return false;
    return RESTART_FIELDS.some((key) => draft[key] !== savedDraft[key]);
  }, [draft, savedDraft]);

  const updateDraft = <K extends DraftKey>(key: K, value: DraftConfig[K]) => {
    patchKey(key, value);
    setErrors((prev) => ({ ...prev, [key]: undefined }));
  };

  const saveChanges = async () => {
    if (!draft) return;
    const nextErrors = validateDraft(draft);
    if (hasErrors(nextErrors)) {
      setErrors(nextErrors);
      return;
    }

    const configPatch = { ...draftPatch };
    delete configPatch.interval;
    const payload = toPatchPayload(configPatch);
    const hasConfigChanges = Object.keys(payload).length > 0;
    const hasTimerChange = draftPatch.interval !== undefined;
    const hasFTSSearchChange = draftPatch.fts_search !== undefined;
    const hasIntegrityCheckChange = draftPatch.integrity_check !== undefined;

    if (!hasConfigChanges && !hasTimerChange) return;

    try {
      let nextConfig: IndexerConfig | undefined;
      let nextRestartRequired = false;

      if (hasConfigChanges) {
        const configResult = await setConfigMutation.mutateAsync(payload);
        nextConfig = configResult.config;
        nextRestartRequired = configResult.restart_required;
      }

      if (hasTimerChange) {
        const timerResult = await setTimerMutation.mutateAsync({
          interval: draft.interval.trim(),
        });
        nextConfig = timerResult.config;
      }

      if (nextConfig) {
        indexerConfigCache.set(nextConfig);
      }
      setDraftPatch({});
      setErrors({});
      setRestartRequired(nextRestartRequired);
      toast.success(
        hasTimerChange && !hasConfigChanges
          ? "Indexer timer saved"
          : "Indexer settings saved",
      );
      if (nextRestartRequired) {
        toast.info("Restart indexer to apply database or listener changes.");
      }
      if (hasFTSSearchChange) {
        toast.info("Run a full index to apply the selected search mode.");
      }
      if (hasIntegrityCheckChange) {
        toast.info(
          "The integrity check mode will apply on the next indexing run.",
        );
      }
      void refetchStatus();
      void refetchTimer();
    } catch {
      // Error toasts are owned by the ActionConfig `error` strings; the catch
      // only stops the sequence so a failed step keeps the draft dirty.
    }
  };

  const handleSave = () => {
    void saveChanges();
  };

  const handleRefresh = () => {
    void refetch();
    void refetchStatus();
    void refetchTimer();
  };

  const renderStatusGrid = (children: ReactNode) => (
    <div
      className="indexer-status-grid"
      style={{
        display: "grid",
        columnGap: theme.spacing(1.5),
        rowGap: theme.spacing(1.5),
      }}
    >
      {children}
    </div>
  );

  const shellProps = {
    title: "Indexer",
    subtitle: "Filesystem search, folder sizes, and index storage.",
    refreshAriaLabel: "Refresh indexer settings",
    refreshing,
    refreshDisabled: !indexerEnabled,
    onRefresh: handleRefresh,
  };

  if (!indexerEnabled) {
    return (
      <SettingsSectionShell {...shellProps}>
        <AppAlert severity={indexerStatus === "unknown" ? "info" : "warning"}>
          <AppAlertTitle>
            {indexerStatus === "unknown"
              ? "Checking Indexer"
              : "Indexer unavailable"}
          </AppAlertTitle>
          {indexerReason}
        </AppAlert>
      </SettingsSectionShell>
    );
  }

  if (error) {
    return (
      <SettingsSectionShell {...shellProps}>
        <AppAlert severity="error">
          <AppAlertTitle>Indexer settings unavailable</AppAlertTitle>
          {error.message}
        </AppAlert>
      </SettingsSectionShell>
    );
  }

  if (isLoading || !draft) {
    return (
      <SettingsSectionShell {...shellProps}>
        <div style={{ padding: theme.spacing(3) }}>
          <ComponentLoader />
        </div>
      </SettingsSectionShell>
    );
  }

  const ftsStatus = getFTSStatus(
    draft.fts_search,
    daemonStatus?.fts_active ?? false,
  );
  const integrityCheckOption = getIntegrityCheckOption(draft.integrity_check);

  return (
    <SettingsSectionShell {...shellProps}>
      {restartRequired ? (
        <AppAlert severity="info">
          <AppAlertTitle>Restart required</AppAlertTitle>
          Some saved settings need the indexer daemon to restart before they
          fully apply.
        </AppAlert>
      ) : willRequireRestart ? (
        <AppAlert severity="info">
          Some changed settings will require an indexer restart after saving.
        </AppAlert>
      ) : null}

      <SectionCard
        icon="mdi:chart-box-outline"
        indicator={
          daemonStatus ? (
            <StatusDot
              absolute
              color={getStatusColor(daemonStatus, theme)}
              style={{ top: 16, right: 12 }}
              tooltip={statusTooltip}
            />
          ) : null
        }
        subtitle="Daemon state, indexed entries, storage info"
        title="Indexer Status"
      >
        {statusError ? (
          <AppAlert severity="warning">
            <AppAlertTitle>Status unavailable</AppAlertTitle>
            {statusError.message}
          </AppAlert>
        ) : daemonStatus ? (
          <>
            {renderStatusGrid(
              <>
                <StatusMetric
                  detail={daemonStatus.active_path}
                  label="State"
                  value={formatActiveIndexerStatus(daemonStatus)}
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
                <StatusMetric
                  detail={ftsStatus.detail}
                  label="Search mode"
                  value={ftsStatus.label}
                />
              </>,
            )}
            {daemonStatus.warning ? (
              <div style={{ marginTop: theme.spacing(1.5) }}>
                <AppAlert severity="warning">{daemonStatus.warning}</AppAlert>
              </div>
            ) : null}
          </>
        ) : (
          <div style={{ padding: theme.spacing(1) }}>
            <ComponentLoader />
          </div>
        )}
      </SectionCard>

      <div className="indexer-toggle-grid">
        <ToggleCard
          checked={draft.include_hidden}
          description="Scan dotfiles and dot directories"
          disabled={busy}
          label="Include hidden files"
          onChange={(checked) => updateDraft("include_hidden", checked)}
        />
        <ToggleCard
          checked={draft.include_network_mounts}
          description="Allows NFS, SMB and CIFS"
          disabled={busy}
          label="Include network mounts"
          onChange={(checked) => updateDraft("include_network_mounts", checked)}
        />
        <ToggleCard
          checked={draft.fresh_index}
          description="Clears database before indexing"
          disabled={busy}
          label="Fresh index"
          onChange={(checked) => updateDraft("fresh_index", checked)}
        />
        <ToggleCard
          checked={draft.fts_search}
          description="Faster search; slower indexing."
          disabled={busy}
          label="Fast search"
          onChange={(checked) => updateDraft("fts_search", checked)}
        />
      </div>

      <SectionCard
        icon="mdi:magnify-scan"
        subtitle="Root path, index name, and retention"
        title="Index Scope"
      >
        <SettingsGrid minColumnWidth={180}>
          <>
            <AppTooltip title="Absolute filesystem path">
              <AppTextField
                disabled={busy}
                error={Boolean(errors.index_path)}
                fullWidth
                helperText={errors.index_path}
                label="Index path"
                onChange={(event) =>
                  updateDraft("index_path", event.target.value)
                }
                size="small"
                value={draft.index_path}
              />
            </AppTooltip>
            <AppTooltip title="Identifier shown in logs">
              <AppTextField
                disabled={busy}
                error={Boolean(errors.index_name)}
                fullWidth
                helperText={errors.index_name}
                label="Index name"
                onChange={(event) =>
                  updateDraft("index_name", event.target.value)
                }
                size="small"
                value={draft.index_name}
              />
            </AppTooltip>
            <AppTooltip title="0 disables pruning">
              <AppTextField
                disabled={busy}
                error={Boolean(errors.keep_indexes)}
                fullWidth
                helperText={errors.keep_indexes}
                label="Keep indexes"
                onChange={(event) =>
                  updateDraft("keep_indexes", event.target.value)
                }
                size="small"
                type="number"
                value={draft.keep_indexes}
              />
            </AppTooltip>
          </>
        </SettingsGrid>
      </SectionCard>

      <SectionCard
        icon="mdi:timer-cog-outline"
        indicator={
          timerInfo ? (
            <StatusDot
              absolute
              color={getTimerColor(timerInfo, theme)}
              style={{ top: 16, right: 12 }}
              tooltip={timerTooltip}
            />
          ) : null
        }
        subtitle={INDEXER_TIMER_UNIT}
        title="Auto-Index Timer"
      >
        <SettingsGrid minColumnWidth={160}>
          <>
            <AppTooltip title="Systemd timer cadence; use 0 to disable">
              <AppTextField
                disabled={busy}
                error={Boolean(errors.interval)}
                fullWidth
                helperText={errors.interval}
                label="Timer interval"
                onChange={(event) =>
                  updateDraft("interval", event.target.value)
                }
                size="small"
                value={draft.interval}
              />
            </AppTooltip>
            {timerInfo ? (
              <>
                <StatusMetric
                  label="State"
                  value={formatTimerState(timerInfo)}
                />
                <StatusMetric
                  label="Auto-start"
                  value={formatStatusLabel(
                    String(timerInfo.UnitFileState ?? ""),
                  )}
                />
                <StatusMetric
                  label="Next run"
                  value={formatTimerTimestamp(
                    timerInfo.NextElapseUSec,
                    "Not scheduled",
                  )}
                />
                <StatusMetric
                  label="Last run"
                  value={formatTimerTimestamp(
                    timerInfo.LastTriggerUSec,
                    "Never",
                  )}
                />
              </>
            ) : null}
          </>
        </SettingsGrid>
        {timerError ? (
          <div style={{ marginTop: theme.spacing(1.5) }}>
            <AppAlert severity="warning">
              <AppAlertTitle>Timer unavailable</AppAlertTitle>
              {timerError.message}
            </AppAlert>
          </div>
        ) : !timerInfo ? (
          <div style={{ padding: theme.spacing(1) }}>
            <ComponentLoader />
          </div>
        ) : null}
      </SectionCard>

      <SectionCard
        icon="mdi:connection"
        subtitle="Local socket and optional TCP listener"
        title="Daemon Access"
      >
        <SettingsGrid>
          <>
            <AppTextField
              disabled={busy}
              error={Boolean(errors.socket_path)}
              fullWidth
              label="Socket path"
              onChange={(event) =>
                updateDraft("socket_path", event.target.value)
              }
              size="small"
              value={draft.socket_path}
            />
            <AppTextField
              disabled={busy}
              error={Boolean(errors.listen_addr)}
              fullWidth
              label="Listen address"
              onChange={(event) =>
                updateDraft("listen_addr", event.target.value)
              }
              placeholder=":8080"
              shrinkLabel
              size="small"
              value={draft.listen_addr}
            />
          </>
        </SettingsGrid>
      </SectionCard>

      <SectionCard
        icon="mdi:database-cog-outline"
        subtitle="SQLite integrity checks, path, durability, and connection pool."
        title="Database"
      >
        <SettingsGrid minColumnWidth={220} rowGap={2.75}>
          <>
            <AppTextField
              disabled={busy}
              error={Boolean(errors.db_path)}
              fullWidth
              label="Database path"
              onChange={(event) => updateDraft("db_path", event.target.value)}
              size="small"
              value={draft.db_path}
            />
            <AppTooltip title={integrityCheckOption.description}>
              <AppSelect
                disabled={busy}
                fullWidth
                label="Integrity check"
                onChange={(event) =>
                  updateDraft(
                    "integrity_check",
                    event.target.value as IndexerIntegrityCheck,
                  )
                }
                size="small"
                value={draft.integrity_check}
              >
                {INDEXER_INTEGRITY_CHECK_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </AppSelect>
            </AppTooltip>
            <AppTextField
              disabled={busy}
              error={Boolean(errors.db_busy_timeout)}
              fullWidth
              label="Busy timeout"
              onChange={(event) =>
                updateDraft("db_busy_timeout", event.target.value)
              }
              size="small"
              value={draft.db_busy_timeout}
            />
            <AppTextField
              disabled={busy}
              error={Boolean(errors.db_max_open_conns)}
              fullWidth
              label="Max open connections"
              onChange={(event) =>
                updateDraft("db_max_open_conns", event.target.value)
              }
              size="small"
              type="number"
              value={draft.db_max_open_conns}
            />
            <AppTextField
              disabled={busy}
              error={Boolean(errors.db_max_idle_conns)}
              fullWidth
              label="Max idle connections"
              onChange={(event) =>
                updateDraft("db_max_idle_conns", event.target.value)
              }
              size="small"
              type="number"
              value={draft.db_max_idle_conns}
            />
            <AppTextField
              disabled={busy}
              error={Boolean(errors.db_conn_max_idle_time)}
              fullWidth
              label="Connection idle time"
              onChange={(event) =>
                updateDraft("db_conn_max_idle_time", event.target.value)
              }
              size="small"
              value={draft.db_conn_max_idle_time}
            />
            <AppSelect
              disabled={busy}
              fullWidth
              label="Journal mode"
              onChange={(event) =>
                updateDraft("db_journal_mode", event.target.value)
              }
              size="small"
              value={draft.db_journal_mode}
            >
              {JOURNAL_MODES.map((mode) => (
                <option key={mode} value={mode}>
                  {DB_MODE_LABELS[mode] ?? mode}
                </option>
              ))}
            </AppSelect>
            <AppSelect
              disabled={busy}
              fullWidth
              label="Synchronous"
              onChange={(event) =>
                updateDraft("db_synchronous", event.target.value)
              }
              size="small"
              value={draft.db_synchronous}
            >
              {SYNCHRONOUS_MODES.map((mode) => (
                <option key={mode} value={mode}>
                  {DB_MODE_LABELS[mode] ?? mode}
                </option>
              ))}
            </AppSelect>
            <AppSelect
              disabled={busy}
              fullWidth
              label="Auto vacuum"
              onChange={(event) =>
                updateDraft("db_auto_vacuum", event.target.value)
              }
              size="small"
              value={draft.db_auto_vacuum}
            >
              {AUTO_VACUUM_MODES.map((mode) => (
                <option key={mode} value={mode}>
                  {DB_MODE_LABELS[mode] ?? mode}
                </option>
              ))}
            </AppSelect>
          </>
        </SettingsGrid>
        {draft.integrity_check === "off" ? (
          <div style={{ marginTop: theme.spacing(1.5) }}>
            <AppAlert severity="warning">
              <AppAlertTitle>Integrity checks disabled</AppAlertTitle>
              Database corruption will not be checked before indexing and may
              only be detected when another database operation fails.
            </AppAlert>
          </div>
        ) : null}
      </SectionCard>

      <SettingsSaveFooter
        busy={busy}
        isDirty={isDirty}
        onReset={handleReset}
        onSave={handleSave}
        saveDisabled={hasErrors(errors)}
        saving={setConfigMutation.isPending || setTimerMutation.isPending}
      />
    </SettingsSectionShell>
  );
};

export default IndexerSettingsSection;
