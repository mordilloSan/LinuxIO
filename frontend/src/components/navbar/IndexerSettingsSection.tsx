import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useMemo, useState } from "react";
import { toast } from "sonner";

import {
  linuxio,
  CACHE_TTL_MS,
  type IndexerConfig,
  type IndexerDaemonStatus,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useCapability } from "@/hooks/useCapabilities";
import { type AppTheme, useAppTheme } from "@/theme";
import { formatDate, formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

import "./indexer-settings-section.css";

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

const GO_DURATION_PART_PATTERN = /(-?\d+(?:\.\d+)?)(ns|us|µs|μs|ms|s|m|h)/g;

const compactGoDuration = (value: string) => {
  const trimmed = value.trim();
  if (trimmed === "0") return "0";

  const parts: string[] = [];
  let index = 0;
  let matched = false;

  for (const match of trimmed.matchAll(GO_DURATION_PART_PATTERN)) {
    matched = true;
    if (match.index !== index) return trimmed;
    index = match.index + match[0].length;
    if (Number(match[1]) !== 0) {
      parts.push(match[0]);
    }
  }

  if (!matched || index !== trimmed.length) return trimmed;
  return parts.length > 0 ? parts.join("") : "0";
};

const toDraft = (config: IndexerConfig): DraftConfig => ({
  ...config,
  interval: compactGoDuration(config.interval),
  keep_indexes: String(config.keep_indexes),
  db_max_open_conns: String(config.db_max_open_conns),
  db_max_idle_conns: String(config.db_max_idle_conns),
});

const toPayload = (draft: DraftConfig): IndexerConfig => ({
  ...draft,
  index_path: draft.index_path.trim(),
  index_name: draft.index_name.trim(),
  keep_indexes: Number(draft.keep_indexes),
  db_path: draft.db_path.trim(),
  db_busy_timeout: draft.db_busy_timeout.trim(),
  db_journal_mode: draft.db_journal_mode.trim(),
  db_synchronous: draft.db_synchronous.trim(),
  db_auto_vacuum: draft.db_auto_vacuum.trim(),
  db_max_open_conns: Number(draft.db_max_open_conns),
  db_max_idle_conns: Number(draft.db_max_idle_conns),
  db_conn_max_idle_time: draft.db_conn_max_idle_time.trim(),
  socket_path: draft.socket_path.trim(),
  listen_addr: draft.listen_addr.trim(),
  interval: draft.interval.trim(),
});

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

  if (!draft.interval.trim()) {
    errors.interval = "Interval is required. Use 0 to disable.";
  }

  if (!isNonNegativeInteger(draft.keep_indexes)) {
    errors.keep_indexes = "Use a non-negative whole number.";
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

const draftsEqual = (left: DraftConfig | null, right: DraftConfig | null) =>
  JSON.stringify(left) === JSON.stringify(right);

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

const StatusMetric: React.FC<{
  label: string;
  value: React.ReactNode;
  detail?: React.ReactNode;
}> = ({ label, value, detail }) => {
  const title =
    typeof value === "string" || typeof value === "number"
      ? String(value)
      : undefined;
  return (
    <div style={{ minWidth: 0 }}>
      <AppTypography variant="caption" color="text.secondary">
        {label}
      </AppTypography>
      <AppTypography variant="body2" fontWeight={600} noWrap title={title}>
        {value}
      </AppTypography>
      {detail ? (
        <AppTypography variant="caption" color="text.secondary" noWrap>
          {detail}
        </AppTypography>
      ) : null}
    </div>
  );
};

const SectionCard: React.FC<{
  icon: string;
  title: string;
  subtitle: string;
  indicator?: React.ReactNode;
  children: React.ReactNode;
}> = ({ icon, title, subtitle, indicator, children }) => {
  const theme = useAppTheme();
  return (
    <FrostedCard style={{ padding: 12, position: "relative" }}>
      {indicator}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(1.5),
          marginBottom: theme.spacing(2.75),
        }}
      >
        <div
          style={{
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            width: 36,
            height: 36,
            borderRadius: 8,
            background: theme.palette.action.hover,
            color: theme.palette.primary.main,
            flexShrink: 0,
          }}
        >
          <Icon icon={icon} width={22} height={22} />
        </div>
        <div>
          <AppTypography variant="body2" fontWeight={600} component="h3">
            {title}
          </AppTypography>
          <AppTypography variant="caption" color="text.secondary">
            {subtitle}
          </AppTypography>
        </div>
      </div>
      {children}
    </FrostedCard>
  );
};

const ToggleCard: React.FC<{
  label: string;
  description: string;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}> = ({ label, description, checked, disabled, onChange }) => {
  const theme = useAppTheme();
  return (
    <FrostedCard
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: theme.spacing(1.5),
        minHeight: 62,
        padding: 12,
      }}
    >
      <div style={{ minWidth: 0 }}>
        <AppTypography
          variant="body2"
          fontWeight={600}
          style={{ lineHeight: 1.25 }}
        >
          {label}
        </AppTypography>
        <AppTypography
          variant="caption"
          color="text.secondary"
          noWrap
          style={{ lineHeight: 1.35 }}
        >
          {description}
        </AppTypography>
      </div>
      <AppSwitch
        checked={checked}
        disabled={disabled}
        onChange={(_, nextChecked) => onChange(nextChecked)}
        aria-label={label}
      />
    </FrostedCard>
  );
};

const IndexerSettingsSection: React.FC = () => {
  const theme = useAppTheme();
  const queryClient = useQueryClient();
  const {
    isEnabled: indexerEnabled,
    status: indexerStatus,
    reason: indexerReason,
  } = useCapability("indexerAvailable");
  const [draftPatch, setDraftPatch] = useState<Partial<DraftConfig>>({});
  const [errors, setErrors] = useState<DraftErrors>({});
  const [restartRequired, setRestartRequired] = useState(false);

  const {
    data: config,
    isPending,
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
    args: [config?.db_path ?? ""],
    enabled: indexerEnabled && Boolean(config?.db_path),
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });

  const setConfigMutation = linuxio.indexer.set_config.useMutation({
    onSuccess: (result) => {
      queryClient.setQueryData(
        linuxio.indexer.get_config.queryKey(),
        result.config,
      );
      setDraftPatch({});
      setErrors({});
      setRestartRequired(result.restart_required);
      toast.success("Indexer settings saved");
      if (result.restart_required) {
        toast.info("Restart indexer to apply database or listener changes.");
      }
    },
    onError: (err) => {
      toast.error(
        getMutationErrorMessage(err, "Failed to save indexer settings"),
      );
    },
  });

  const savedDraft = useMemo(() => (config ? toDraft(config) : null), [config]);
  const draft = useMemo(
    () => (savedDraft ? { ...savedDraft, ...draftPatch } : null),
    [draftPatch, savedDraft],
  );
  const isDirty = !draftsEqual(draft, savedDraft);
  const busy = isFetching || setConfigMutation.isPending;
  const refreshing = busy || isStatusFetching;
  const statusTooltip = formatStatusLabel(daemonStatus?.status);
  const willRequireRestart = useMemo(() => {
    if (!draft || !savedDraft) return false;
    return RESTART_FIELDS.some((key) => draft[key] !== savedDraft[key]);
  }, [draft, savedDraft]);

  const updateDraft = <K extends DraftKey>(key: K, value: DraftConfig[K]) => {
    setDraftPatch((prev) => {
      if (!savedDraft) return prev;
      if (Object.is(savedDraft[key], value)) {
        const next = { ...prev };
        delete next[key];
        return next;
      }
      return { ...prev, [key]: value };
    });
    setErrors((prev) => ({ ...prev, [key]: undefined }));
    setRestartRequired(false);
  };

  const handleReset = () => {
    setDraftPatch({});
    setErrors({});
    setRestartRequired(false);
  };

  const handleSave = () => {
    if (!draft) return;
    const nextErrors = validateDraft(draft);
    if (hasErrors(nextErrors)) {
      setErrors(nextErrors);
      return;
    }
    setConfigMutation.mutate([toPayload(draft)]);
  };

  const handleRefresh = () => {
    void refetch();
    void refetchStatus();
  };

  const renderGrid = (
    children: React.ReactNode,
    minColumnWidth = 220,
    rowGap = 1.5,
  ) => (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: `repeat(auto-fit, minmax(${minColumnWidth}px, 1fr))`,
        columnGap: theme.spacing(1.5),
        rowGap: theme.spacing(rowGap),
      }}
    >
      {children}
    </div>
  );

  const renderStatusGrid = (children: React.ReactNode) => (
    <div
      className="indexer-status-grid"
      style={{
        display: "grid",
        rowGap: theme.spacing(1.5),
      }}
    >
      {children}
    </div>
  );

  const header = (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: theme.spacing(1.5),
      }}
    >
      <div>
        <AppTypography variant="body1" fontWeight={600}>
          Indexer
        </AppTypography>
        <AppTypography variant="caption" color="text.secondary">
          Filesystem search, folder sizes, and index storage.
        </AppTypography>
      </div>
      <AppTooltip title={refreshing ? "Refreshing" : "Refresh"}>
        <AppIconButton
          size="small"
          disabled={refreshing || !indexerEnabled}
          aria-label="Refresh indexer settings"
          onClick={handleRefresh}
        >
          <Icon
            icon={refreshing ? "mdi:loading" : "mdi:refresh"}
            width={18}
            height={18}
          />
        </AppIconButton>
      </AppTooltip>
    </div>
  );

  if (!indexerEnabled) {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(1.5),
        }}
      >
        {header}
        <AppAlert severity={indexerStatus === "unknown" ? "info" : "warning"}>
          <AppAlertTitle>
            {indexerStatus === "unknown"
              ? "Checking Indexer"
              : "Indexer unavailable"}
          </AppAlertTitle>
          {indexerReason}
        </AppAlert>
      </div>
    );
  }

  if (error) {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(1.5),
        }}
      >
        {header}
        <AppAlert severity="error">
          <AppAlertTitle>Indexer settings unavailable</AppAlertTitle>
          {error.message}
        </AppAlert>
      </div>
    );
  }

  if (isPending || !draft) {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(1.5),
        }}
      >
        {header}
        <div style={{ padding: theme.spacing(3) }}>
          <ComponentLoader />
        </div>
      </div>
    );
  }

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: theme.spacing(1.5),
      }}
    >
      {header}

      {restartRequired ? (
        <AppAlert severity="info">
          <AppAlertTitle>Restart required</AppAlertTitle>
          Some saved settings need the indexer service to restart before they
          fully apply.
        </AppAlert>
      ) : willRequireRestart ? (
        <AppAlert severity="info">
          Some changed settings will require an indexer restart after saving.
        </AppAlert>
      ) : null}

      <SectionCard
        icon="mdi:chart-box-outline"
        title="Indexer Status"
        subtitle="Daemon state, indexed entries, storage info"
        indicator={
          daemonStatus ? (
            <StatusDot
              color={getStatusColor(daemonStatus, theme)}
              tooltip={statusTooltip}
              absolute
              style={{ top: 16, right: 12 }}
            />
          ) : null
        }
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
          label="Include hidden files"
          description="Scan dotfiles and dot directories"
          checked={draft.include_hidden}
          disabled={busy}
          onChange={(checked) => updateDraft("include_hidden", checked)}
        />
        <ToggleCard
          label="Include network mounts"
          description="Allows NFS, SMB and CIFS"
          checked={draft.include_network_mounts}
          disabled={busy}
          onChange={(checked) => updateDraft("include_network_mounts", checked)}
        />
        <ToggleCard
          label="Fresh index"
          description="Clears database before indexing"
          checked={draft.fresh_index}
          disabled={busy}
          onChange={(checked) => updateDraft("fresh_index", checked)}
        />
      </div>

      <SectionCard
        icon="mdi:magnify-scan"
        title="Index Scope"
        subtitle="Root path, scan cadence, and inclusion rules"
      >
        {renderGrid(
          <>
            <AppTooltip title="Absolute filesystem path">
              <AppTextField
                label="Index path"
                size="small"
                value={draft.index_path}
                onChange={(event) =>
                  updateDraft("index_path", event.target.value)
                }
                error={Boolean(errors.index_path)}
                helperText={errors.index_path}
                disabled={busy}
                fullWidth
              />
            </AppTooltip>
            <AppTooltip title="Identifier shown in logs">
              <AppTextField
                label="Index name"
                size="small"
                value={draft.index_name}
                onChange={(event) =>
                  updateDraft("index_name", event.target.value)
                }
                error={Boolean(errors.index_name)}
                helperText={errors.index_name}
                disabled={busy}
                fullWidth
              />
            </AppTooltip>
            <AppTooltip
              title={
                <>
                  Auto-index interval: 30m, 1h, or 6h.
                  <br />
                  Use 0 to disable.
                </>
              }
            >
              <AppTextField
                label="Interval"
                size="small"
                value={draft.interval}
                onChange={(event) =>
                  updateDraft("interval", event.target.value)
                }
                error={Boolean(errors.interval)}
                helperText={errors.interval}
                disabled={busy}
                fullWidth
              />
            </AppTooltip>
            <AppTooltip title="0 disables pruning">
              <AppTextField
                label="Keep indexes"
                size="small"
                type="number"
                value={draft.keep_indexes}
                onChange={(event) =>
                  updateDraft("keep_indexes", event.target.value)
                }
                error={Boolean(errors.keep_indexes)}
                helperText={errors.keep_indexes}
                disabled={busy}
                fullWidth
              />
            </AppTooltip>
          </>,
          180,
        )}
      </SectionCard>

      <SectionCard
        icon="mdi:connection"
        title="Daemon Access"
        subtitle="Local socket and optional TCP listener"
      >
        {renderGrid(
          <>
            <AppTextField
              label="Socket path"
              size="small"
              value={draft.socket_path}
              onChange={(event) =>
                updateDraft("socket_path", event.target.value)
              }
              error={Boolean(errors.socket_path)}
              disabled={busy}
              fullWidth
            />
            <AppTextField
              label="Listen address"
              size="small"
              value={draft.listen_addr}
              onChange={(event) =>
                updateDraft("listen_addr", event.target.value)
              }
              placeholder=":8080"
              shrinkLabel
              error={Boolean(errors.listen_addr)}
              disabled={busy}
              fullWidth
            />
          </>,
        )}
      </SectionCard>

      <SectionCard
        icon="mdi:database-cog-outline"
        title="Database"
        subtitle="SQLite path, durability, and connection pool."
      >
        {renderGrid(
          <>
            <AppTextField
              label="Database path"
              size="small"
              value={draft.db_path}
              onChange={(event) => updateDraft("db_path", event.target.value)}
              error={Boolean(errors.db_path)}
              disabled={busy}
              fullWidth
            />
            <AppTextField
              label="Busy timeout"
              size="small"
              value={draft.db_busy_timeout}
              onChange={(event) =>
                updateDraft("db_busy_timeout", event.target.value)
              }
              error={Boolean(errors.db_busy_timeout)}
              disabled={busy}
              fullWidth
            />
            <AppTextField
              label="Max open connections"
              size="small"
              type="number"
              value={draft.db_max_open_conns}
              onChange={(event) =>
                updateDraft("db_max_open_conns", event.target.value)
              }
              error={Boolean(errors.db_max_open_conns)}
              disabled={busy}
              fullWidth
            />
            <AppTextField
              label="Max idle connections"
              size="small"
              type="number"
              value={draft.db_max_idle_conns}
              onChange={(event) =>
                updateDraft("db_max_idle_conns", event.target.value)
              }
              error={Boolean(errors.db_max_idle_conns)}
              disabled={busy}
              fullWidth
            />
            <AppTextField
              label="Connection idle time"
              size="small"
              value={draft.db_conn_max_idle_time}
              onChange={(event) =>
                updateDraft("db_conn_max_idle_time", event.target.value)
              }
              error={Boolean(errors.db_conn_max_idle_time)}
              disabled={busy}
              fullWidth
            />
            <AppSelect
              label="Journal mode"
              size="small"
              value={draft.db_journal_mode}
              onChange={(event) =>
                updateDraft("db_journal_mode", event.target.value)
              }
              disabled={busy}
              fullWidth
            >
              {JOURNAL_MODES.map((mode) => (
                <option key={mode} value={mode}>
                  {DB_MODE_LABELS[mode] ?? mode}
                </option>
              ))}
            </AppSelect>
            <AppSelect
              label="Synchronous"
              size="small"
              value={draft.db_synchronous}
              onChange={(event) =>
                updateDraft("db_synchronous", event.target.value)
              }
              disabled={busy}
              fullWidth
            >
              {SYNCHRONOUS_MODES.map((mode) => (
                <option key={mode} value={mode}>
                  {DB_MODE_LABELS[mode] ?? mode}
                </option>
              ))}
            </AppSelect>
            <AppSelect
              label="Auto vacuum"
              size="small"
              value={draft.db_auto_vacuum}
              onChange={(event) =>
                updateDraft("db_auto_vacuum", event.target.value)
              }
              disabled={busy}
              fullWidth
            >
              {AUTO_VACUUM_MODES.map((mode) => (
                <option key={mode} value={mode}>
                  {DB_MODE_LABELS[mode] ?? mode}
                </option>
              ))}
            </AppSelect>
          </>,
          220,
          2.75,
        )}
      </SectionCard>

      <div
        style={{
          display: "flex",
          justifyContent: "flex-end",
          gap: theme.spacing(1.5),
          paddingTop: theme.spacing(0.5),
        }}
      >
        <AppButton onClick={handleReset} disabled={!isDirty || busy}>
          Reset
        </AppButton>
        <AppButton
          variant="contained"
          onClick={handleSave}
          disabled={!isDirty || busy || hasErrors(errors)}
          startIcon={
            <Icon icon="mdi:content-save-outline" width={18} height={18} />
          }
        >
          {setConfigMutation.isPending ? "Saving..." : "Save"}
        </AppButton>
      </div>
    </div>
  );
};

export default IndexerSettingsSection;
