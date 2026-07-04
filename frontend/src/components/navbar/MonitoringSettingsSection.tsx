import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useMemo, useState } from "react";
import { toast } from "sonner";

import {
  CACHE_TTL_MS,
  jobSnapshotResult,
  linuxio,
  type MonitoringConfig,
  type MonitoringConfigPatch,
  type MonitoringListener,
} from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import {
  SectionCard,
  StatusMetric,
  ToggleCard,
} from "@/components/navbar/SettingsSectionPrimitives";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useCapability } from "@/hooks/useCapabilities";
import { useAppTheme } from "@/theme";
import { compactGoDuration, isGoDuration } from "@/utils/durations";
import { getMutationErrorMessage } from "@/utils/mutations";

interface DraftConfig {
  collector_interval: string;
  history: string;
  allow_remote_commands: boolean;
  cache_ttl: Record<string, string>;
  listeners: MonitoringListener[];
}

interface DraftErrors {
  collector_interval?: string;
  cache_ttl?: Partial<Record<string, string>>;
  listener_addresses?: Partial<Record<number, string>>;
}

const toDraft = (config: MonitoringConfig): DraftConfig => ({
  collector_interval: compactGoDuration(config.collector_interval),
  history: config.history,
  allow_remote_commands: config.allow_remote_commands,
  cache_ttl: Object.fromEntries(
    Object.entries(config.cache_ttl ?? {}).map(([key, value]) => [
      key,
      compactGoDuration(value),
    ]),
  ),
  listeners: (config.listeners ?? []).map((listener) => ({
    ...listener,
    apis: [...listener.apis],
  })),
});

const normalizeListeners = (listeners: MonitoringListener[]) =>
  listeners.map((listener) => ({
    ...listener,
    address: listener.address.trim(),
    name: listener.name.trim(),
    apis: listener.apis.map((api) => api.trim()).filter(Boolean),
  }));

const toPatchPayload = (
  draft: DraftConfig,
  saved: DraftConfig,
): MonitoringConfigPatch => {
  const payload: MonitoringConfigPatch = {};

  if (draft.collector_interval !== saved.collector_interval) {
    payload.collector_interval = draft.collector_interval.trim();
  }
  if (draft.history !== saved.history) {
    payload.history = draft.history.trim();
  }
  if (draft.allow_remote_commands !== saved.allow_remote_commands) {
    payload.allow_remote_commands = draft.allow_remote_commands;
  }
  if (JSON.stringify(draft.listeners) !== JSON.stringify(saved.listeners)) {
    payload.listeners = normalizeListeners(draft.listeners);
  }

  const changedTTLs: Record<string, string> = {};
  for (const [key, value] of Object.entries(draft.cache_ttl)) {
    if (value !== saved.cache_ttl[key]) {
      changedTTLs[key] = value.trim();
    }
  }
  if (Object.keys(changedTTLs).length > 0) {
    payload.cache_ttl = changedTTLs;
  }

  return payload;
};

const isListenerAddress = (value: string) => {
  const address = value.trim();
  if (!address) return false;
  if (["none", "off", "disabled"].includes(address.toLowerCase())) {
    return true;
  }
  if (address.startsWith("unix:/")) return true;
  if (address.startsWith("/")) return true;
  if (/^\d+$/.test(address)) return true;
  return address.includes(":");
};

const validateDraft = (draft: DraftConfig): DraftErrors => {
  const errors: DraftErrors = {};

  const interval = draft.collector_interval.trim();
  if (!interval) {
    errors.collector_interval = "Collector interval is required.";
  } else if (!isGoDuration(interval) || interval === "0") {
    errors.collector_interval = "Use a duration like 15s, 1m, or 5m.";
  }

  const ttlErrors: Partial<Record<string, string>> = {};
  for (const [key, value] of Object.entries(draft.cache_ttl)) {
    if (!value.trim()) {
      ttlErrors[key] = "Required.";
    } else if (!isGoDuration(value)) {
      ttlErrors[key] = "Use a duration like 2s or 0.";
    }
  }
  if (Object.keys(ttlErrors).length > 0) {
    errors.cache_ttl = ttlErrors;
  }

  const listenerErrors: Partial<Record<number, string>> = {};
  draft.listeners.forEach((listener, index) => {
    if (!listener.address.trim()) {
      listenerErrors[index] = "Required.";
    } else if (!isListenerAddress(listener.address)) {
      listenerErrors[index] =
        "Use host:port, :port, port, unix:/path, or /path.";
    }
  });
  if (Object.keys(listenerErrors).length > 0) {
    errors.listener_addresses = listenerErrors;
  }

  return errors;
};

const hasErrors = (errors: DraftErrors) =>
  Boolean(errors.collector_interval) ||
  Object.values(errors.cache_ttl ?? {}).some(Boolean) ||
  Object.values(errors.listener_addresses ?? {}).some(Boolean);

const draftsEqual = (left: DraftConfig | null, right: DraftConfig | null) =>
  JSON.stringify(left) === JSON.stringify(right);

const formatTTLLabel = (key: string) =>
  key
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");

const MonitoringSettingsSection: React.FC = () => {
  const theme = useAppTheme();
  const queryClient = useQueryClient();
  const {
    isEnabled: monitoringEnabled,
    status: monitoringStatus,
    reason: monitoringReason,
  } = useCapability("monitoringAvailable");
  const [draftPatch, setDraftPatch] = useState<Partial<DraftConfig>>({});
  const [errors, setErrors] = useState<DraftErrors>({});
  const [restartRequired, setRestartRequired] = useState(false);

  const {
    data: config,
    isPending,
    error,
    refetch,
    isFetching,
  } = linuxio.monitoring.get_config.useQuery({
    enabled: monitoringEnabled,
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });
  const {
    data: agentStatus,
    error: statusError,
    refetch: refetchStatus,
    isFetching: isStatusFetching,
  } = linuxio.monitoring.get_status.useQuery({
    enabled: monitoringEnabled,
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });

  const setConfigMutation = linuxio.monitoring.set_config.useMutation();
  const restartMutation = linuxio.monitoring.restart.useMutation();

  const savedDraft = useMemo(() => (config ? toDraft(config) : null), [config]);
  const draft = useMemo(
    () =>
      savedDraft
        ? {
            ...savedDraft,
            ...draftPatch,
            cache_ttl: { ...savedDraft.cache_ttl, ...draftPatch.cache_ttl },
          }
        : null,
    [draftPatch, savedDraft],
  );
  const isDirty = !draftsEqual(draft, savedDraft);
  const busy =
    isFetching || setConfigMutation.isPending || restartMutation.isPending;
  const refreshing = busy || isStatusFetching;

  const updateDraft = <K extends keyof DraftConfig>(
    key: K,
    value: DraftConfig[K],
  ) => {
    setDraftPatch((prev) => {
      if (!savedDraft) return prev;
      if (Object.is(savedDraft[key], value)) {
        const next = { ...prev };
        delete next[key];
        return next;
      }
      return { ...prev, [key]: value };
    });
    if (key === "collector_interval") {
      setErrors((prev) => ({ ...prev, collector_interval: undefined }));
    }
    setRestartRequired(false);
  };

  const updateCacheTTL = (key: string, value: string) => {
    setDraftPatch((prev) => {
      if (!savedDraft) return prev;
      const nextTTLs = { ...prev.cache_ttl };
      if (savedDraft.cache_ttl[key] === value) {
        delete nextTTLs[key];
      } else {
        nextTTLs[key] = value;
      }
      if (Object.keys(nextTTLs).length === 0) {
        const next = { ...prev };
        delete next.cache_ttl;
        return next;
      }
      return { ...prev, cache_ttl: nextTTLs };
    });
    setErrors((prev) => ({
      ...prev,
      cache_ttl: { ...prev.cache_ttl, [key]: undefined },
    }));
    setRestartRequired(false);
  };

  const updateListenerAddress = (index: number, address: string) => {
    setDraftPatch((prev) => {
      if (!savedDraft) return prev;
      const baseListeners = prev.listeners ?? savedDraft.listeners;
      const nextListeners = baseListeners.map((listener, listenerIndex) =>
        listenerIndex === index ? { ...listener, address } : listener,
      );
      if (
        JSON.stringify(nextListeners) === JSON.stringify(savedDraft.listeners)
      ) {
        const next = { ...prev };
        delete next.listeners;
        return next;
      }
      return { ...prev, listeners: nextListeners };
    });
    setErrors((prev) => ({
      ...prev,
      listener_addresses: { ...prev.listener_addresses, [index]: undefined },
    }));
    setRestartRequired(false);
  };

  const handleReset = () => {
    setDraftPatch({});
    setErrors({});
    setRestartRequired(false);
  };

  const saveChanges = async () => {
    if (!draft || !savedDraft) return;
    const nextErrors = validateDraft(draft);
    if (hasErrors(nextErrors)) {
      setErrors(nextErrors);
      return;
    }

    const payload = toPatchPayload(draft, savedDraft);
    if (Object.keys(payload).length === 0) return;

    try {
      const result = jobSnapshotResult(
        await setConfigMutation.mutateAsync(payload),
      );
      queryClient.setQueryData(
        linuxio.monitoring.get_config.queryKey(),
        result.config,
      );
      setDraftPatch({});
      setErrors({});
      setRestartRequired(result.restart_required);
      toast.success("Monitoring settings saved");
      if (result.restart_required) {
        toast.info("Restart go-monitoring to apply listener changes.");
      }
      void refetchStatus();
    } catch (err) {
      toast.error(
        getMutationErrorMessage(err, "Failed to save monitoring settings"),
      );
    }
  };

  const handleSave = () => {
    void saveChanges();
  };

  const handleRefresh = () => {
    void refetch();
    void refetchStatus();
  };

  const handleRestart = () => {
    restartMutation.mutate(undefined, {
      onSuccess: () => {
        setRestartRequired(false);
        toast.success("go-monitoring restarted");
        void refetch();
        void refetchStatus();
      },
      onError: (err) => {
        toast.error(
          getMutationErrorMessage(err, "Failed to restart go-monitoring"),
        );
      },
    });
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
        <AppTypography fontWeight={600} variant="body1">
          Monitoring
        </AppTypography>
        <AppTypography color="text.secondary" variant="caption">
          Historical host metrics agent (go-monitoring).
        </AppTypography>
      </div>
      <AppTooltip title={refreshing ? "Refreshing" : "Refresh"}>
        <AppIconButton
          aria-label="Refresh monitoring settings"
          disabled={refreshing || !monitoringEnabled}
          onClick={handleRefresh}
          size="small"
        >
          <Icon
            height={18}
            icon={refreshing ? "mdi:loading" : "mdi:refresh"}
            width={18}
          />
        </AppIconButton>
      </AppTooltip>
    </div>
  );

  if (!monitoringEnabled) {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(1.5),
        }}
      >
        {header}
        <AppAlert
          severity={monitoringStatus === "unknown" ? "info" : "warning"}
        >
          <AppAlertTitle>
            {monitoringStatus === "unknown"
              ? "Checking Monitoring"
              : "Monitoring unavailable"}
          </AppAlertTitle>
          {monitoringReason}
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
          <AppAlertTitle>Monitoring settings unavailable</AppAlertTitle>
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

  const ttlKeys = Object.keys(draft.cache_ttl).sort();
  const editableListeners = draft.listeners
    .map((listener, index) => ({ listener, index }))
    .filter(({ listener }) => listener.apis.includes("metrics"));

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
          Some saved settings need the go-monitoring agent to restart before
          they fully apply.
          <div style={{ marginTop: theme.spacing(1.25) }}>
            <AppButton
              disabled={busy}
              onClick={handleRestart}
              size="small"
              startIcon={
                <Icon
                  height={16}
                  icon={
                    restartMutation.isPending ? "mdi:loading" : "mdi:restart"
                  }
                  width={16}
                />
              }
              variant="outlined"
            >
              {restartMutation.isPending ? "Restarting..." : "Restart agent"}
            </AppButton>
          </div>
        </AppAlert>
      ) : null}

      <SectionCard
        icon="mdi:chart-line"
        indicator={
          <StatusDot
            absolute
            color={theme.palette.success.main}
            style={{ top: 16, right: 12 }}
            tooltip="Agent healthy"
          />
        }
        subtitle="Agent version, storage, and listeners"
        title="Agent Status"
      >
        {statusError ? (
          <AppAlert severity="warning">
            <AppAlertTitle>Status unavailable</AppAlertTitle>
            {statusError.message}
          </AppAlert>
        ) : agentStatus ? (
          <>
            {renderGrid(
              <>
                <StatusMetric label="Version" value={agentStatus.version} />
                <StatusMetric
                  label="Collector interval"
                  value={compactGoDuration(agentStatus.collector_interval)}
                />
                <StatusMetric
                  label="SMART refresh"
                  value={compactGoDuration(agentStatus.smart_refresh_interval)}
                />
                <StatusMetric
                  detail={agentStatus.data_dir}
                  label="Database"
                  value={agentStatus.db_path}
                />
                <StatusMetric
                  detail={agentStatus.config.path}
                  label="Config"
                  value={agentStatus.config.source}
                />
              </>,
              180,
            )}
            {agentStatus.listeners?.length ? (
              <div style={{ marginTop: theme.spacing(1.5) }}>
                {renderGrid(
                  agentStatus.listeners.map((listener) => (
                    <StatusMetric
                      detail={listener.apis.join(", ")}
                      key={listener.name}
                      label={`Listener: ${listener.name}`}
                      value={listener.effective_address || listener.address}
                    />
                  )),
                  220,
                )}
              </div>
            ) : null}
          </>
        ) : (
          <div style={{ padding: theme.spacing(1) }}>
            <ComponentLoader />
          </div>
        )}
      </SectionCard>

      <ToggleCard
        checked={draft.allow_remote_commands}
        description="Allow the command API on non-loopback TCP listeners"
        disabled={busy}
        label="Allow remote commands"
        onChange={(checked) => updateDraft("allow_remote_commands", checked)}
      />

      {editableListeners.length > 0 ? (
        <SectionCard
          icon="mdi:connection"
          subtitle="Metrics API bind addresses"
          title="Listeners"
        >
          {renderGrid(
            editableListeners.map(({ listener, index }) => (
              <AppTextField
                disabled={busy}
                error={Boolean(errors.listener_addresses?.[index])}
                fullWidth
                helperText={errors.listener_addresses?.[index]}
                key={`${listener.name}-${index}`}
                label={`${listener.name} address`}
                onChange={(event) =>
                  updateListenerAddress(index, event.target.value)
                }
                placeholder="0.0.0.0:45876"
                shrinkLabel
                size="small"
                value={listener.address}
              />
            )),
          )}
        </SectionCard>
      ) : null}

      <SectionCard
        icon="mdi:timer-cog-outline"
        subtitle="Sampling cadence and history retention plugins"
        title="Collector"
      >
        {renderGrid(
          <>
            <AppTooltip title="How often metrics are sampled">
              <AppTextField
                disabled={busy}
                error={Boolean(errors.collector_interval)}
                fullWidth
                helperText={errors.collector_interval}
                label="Collector interval"
                onChange={(event) =>
                  updateDraft("collector_interval", event.target.value)
                }
                size="small"
                value={draft.collector_interval}
              />
            </AppTooltip>
            <AppTooltip title="Comma-separated plugins to persist history for">
              <AppTextField
                disabled={busy}
                fullWidth
                label="History plugins"
                onChange={(event) => updateDraft("history", event.target.value)}
                placeholder="cpu,mem,diskio,network"
                shrinkLabel
                size="small"
                value={draft.history}
              />
            </AppTooltip>
          </>,
        )}
      </SectionCard>

      {ttlKeys.length > 0 ? (
        <SectionCard
          icon="mdi:cached"
          subtitle="How long live readings are cached per plugin"
          title="Cache TTLs"
        >
          {renderGrid(
            ttlKeys.map((key) => (
              <AppTextField
                disabled={busy}
                error={Boolean(errors.cache_ttl?.[key])}
                fullWidth
                helperText={errors.cache_ttl?.[key]}
                key={key}
                label={formatTTLLabel(key)}
                onChange={(event) => updateCacheTTL(key, event.target.value)}
                size="small"
                value={draft.cache_ttl[key]}
              />
            )),
            140,
            2.75,
          )}
        </SectionCard>
      ) : null}

      <div
        style={{
          display: "flex",
          justifyContent: "flex-end",
          gap: theme.spacing(1.5),
          paddingTop: theme.spacing(0.5),
        }}
      >
        <AppButton disabled={!isDirty || busy} onClick={handleReset}>
          Reset
        </AppButton>
        <AppButton
          disabled={!isDirty || busy || hasErrors(errors)}
          onClick={handleSave}
          startIcon={
            <Icon height={18} icon="mdi:content-save-outline" width={18} />
          }
          variant="contained"
        >
          {setConfigMutation.isPending ? "Saving..." : "Save"}
        </AppButton>
      </div>
    </div>
  );
};

export default MonitoringSettingsSection;
