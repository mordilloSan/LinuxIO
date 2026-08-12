import { Icon } from "@iconify/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { toast } from "sonner";

import {
  CACHE_TTL_MS,
  linuxio,
  type MonitoringConfig,
  type MonitoringConfigPatch,
  type MonitoringListener,
  useCallMutation,
} from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useCapability } from "@/hooks/useCapabilities";
import { useAppTheme } from "@/theme";
import { compactGoDuration, isGoDuration } from "@/utils/durations";

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

interface DraftConfig {
  collector_interval: string;
  smart_refresh_interval: string;
  history: string;
  allow_remote_commands: boolean;
  cache_ttl: Record<string, string>;
  listeners: MonitoringListener[];
}

interface DraftErrors {
  collector_interval?: string;
  smart_refresh_interval?: string;
  cache_ttl?: Partial<Record<string, string>>;
  listener_addresses?: Partial<Record<number, string>>;
}

const toDraft = (config: MonitoringConfig): DraftConfig => ({
  collector_interval: compactGoDuration(config.collector_interval),
  smart_refresh_interval: compactGoDuration(config.smart_refresh_interval),
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
  if (draft.smart_refresh_interval !== saved.smart_refresh_interval) {
    payload.smart_refresh_interval = draft.smart_refresh_interval.trim();
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

  const smartRefreshInterval = draft.smart_refresh_interval.trim();
  if (!smartRefreshInterval) {
    errors.smart_refresh_interval = "SMART refresh interval is required.";
  } else if (
    !isGoDuration(smartRefreshInterval) ||
    smartRefreshInterval === "0"
  ) {
    errors.smart_refresh_interval = "Use a duration like 1h, 30m, or 12h.";
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
  Boolean(errors.smart_refresh_interval) ||
  Object.values(errors.cache_ttl ?? {}).some(Boolean) ||
  Object.values(errors.listener_addresses ?? {}).some(Boolean);

const mergeMonitoringDraft = (
  saved: DraftConfig,
  patch: Partial<DraftConfig>,
): DraftConfig => ({
  ...saved,
  ...patch,
  cache_ttl: { ...saved.cache_ttl, ...patch.cache_ttl },
});

const formatTTLLabel = (key: string) =>
  key
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");

const getConfigSchemaError = (config: MonitoringConfig): string | null => {
  const data = config as unknown as Record<string, unknown>;
  const requiredDurations = ["collector_interval", "smart_refresh_interval"];
  for (const key of requiredDurations) {
    const value = data[key];
    if (typeof value !== "string") {
      return `monitoring.get_config is missing required string field "${key}".`;
    }
    const trimmed = value.trim();
    if (!trimmed) {
      return `monitoring.get_config field "${key}" must not be empty.`;
    }
    if (!isGoDuration(trimmed) || trimmed === "0") {
      return `monitoring.get_config field "${key}" must be a positive Go duration string.`;
    }
  }

  if (typeof data.history !== "string") {
    return `monitoring.get_config is missing required string field "history".`;
  }

  if (!data.cache_ttl || typeof data.cache_ttl !== "object") {
    return `monitoring.get_config is missing required object field "cache_ttl".`;
  }
  for (const [key, value] of Object.entries(data.cache_ttl)) {
    if (typeof value !== "string") {
      return `monitoring.get_config field "cache_ttl.${key}" must be a string.`;
    }
  }

  if (!Array.isArray(data.listeners)) {
    return `monitoring.get_config is missing required array field "listeners".`;
  }
  return null;
};

const MonitoringSettingsSection = () => {
  const theme = useAppTheme();
  const queryClient = useQueryClient();
  const {
    isEnabled: monitoringEnabled,
    status: monitoringStatus,
    reason: monitoringReason,
  } = useCapability("monitoringAvailable");

  const {
    data: config,
    isLoading,
    error,
    refetch,
    isFetching,
  } = useQuery({
    ...linuxio.monitoring.get_config,
    enabled: monitoringEnabled,
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });
  const {
    data: agentStatus,
    error: statusError,
    refetch: refetchStatus,
    isFetching: isStatusFetching,
  } = useQuery({
    ...linuxio.monitoring.get_status,
    enabled: monitoringEnabled,
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });

  const configSchemaError = config ? getConfigSchemaError(config) : null;
  const savedDraft = useMemo(
    () => (config && !configSchemaError ? toDraft(config) : null),
    [config, configSchemaError],
  );
  const {
    draft,
    setDraftPatch,
    errors,
    setErrors,
    restartRequired,
    setRestartRequired,
    isDirty,
    patchKey,
    reset: handleReset,
  } = useSettingsDraft<DraftConfig, DraftErrors>(
    savedDraft,
    mergeMonitoringDraft,
  );

  const setConfigMutation = useCallMutation(linuxio.monitoring.set_config, {
    success: (result) => {
      queryClient.setQueryData(
        linuxio.monitoring.get_config.queryKey,
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
    },
    error: "Failed to save monitoring settings",
  });
  const restartMutation = useCallMutation(linuxio.monitoring.restart, {
    success: () => {
      setRestartRequired(false);
      toast.success("go-monitoring restarted");
      void refetch();
      void refetchStatus();
    },
    error: "Failed to restart go-monitoring",
  });

  const busy = setConfigMutation.isPending || restartMutation.isPending;
  const refreshing = isFetching || isStatusFetching;

  const updateDraft = <K extends keyof DraftConfig>(
    key: K,
    value: DraftConfig[K],
  ) => {
    patchKey(key, value);
    if (key === "collector_interval" || key === "smart_refresh_interval") {
      setErrors((prev) => ({ ...prev, [key]: undefined }));
    }
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

  // Success/error handling lives in the setConfigMutation ActionConfig.
  const handleSave = () => {
    if (!draft || !savedDraft) return;
    const nextErrors = validateDraft(draft);
    if (hasErrors(nextErrors)) {
      setErrors(nextErrors);
      return;
    }

    const payload = toPatchPayload(draft, savedDraft);
    if (Object.keys(payload).length === 0) return;

    setConfigMutation.mutate(payload);
  };

  const handleRefresh = () => {
    void refetch();
    void refetchStatus();
  };

  const handleRestart = () => {
    restartMutation.mutate();
  };

  const renderAgentStatusCard = () => (
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
      title="Agent Status"
      titleAdornment={
        agentStatus ? (
          <AppTypography
            color="text.secondary"
            component="span"
            variant="caption"
          >
            ({agentStatus.version})
          </AppTypography>
        ) : undefined
      }
    >
      {statusError ? (
        <AppAlert severity="warning">
          <AppAlertTitle>Status unavailable</AppAlertTitle>
          {statusError.message}
        </AppAlert>
      ) : agentStatus ? (
        <>
          <SettingsGrid minColumnWidth={240} rowGap={1}>
            <>
              <StatusMetric label="Database" value={agentStatus.db_path} />
              <StatusMetric
                label={
                  <span
                    style={{
                      display: "inline-flex",
                      alignItems: "center",
                      gap: theme.spacing(0.5),
                    }}
                  >
                    Config
                    <StatusDot
                      color={
                        agentStatus.config.source === "loaded"
                          ? theme.palette.success.main
                          : theme.palette.warning.main
                      }
                      size={7}
                      tooltip={`Config ${agentStatus.config.source}`}
                    />
                  </span>
                }
                value={agentStatus.config.path}
              />
            </>
          </SettingsGrid>
          {agentStatus.listeners?.length ? (
            <div style={{ marginTop: theme.spacing(1.5) }}>
              <SettingsGrid minColumnWidth={240} rowGap={1}>
                {agentStatus.listeners.map((listener) => (
                  <StatusMetric
                    key={listener.name}
                    label={`Listener: ${listener.name}`}
                    value={listener.effective_address || listener.address}
                  />
                ))}
              </SettingsGrid>
            </div>
          ) : null}
        </>
      ) : (
        <div style={{ padding: theme.spacing(1) }}>
          <ComponentLoader />
        </div>
      )}
    </SectionCard>
  );

  const shellProps = {
    title: "Monitoring",
    subtitle: "Historical host metrics agent (go-monitoring).",
    refreshAriaLabel: "Refresh monitoring settings",
    refreshing,
    refreshDisabled: !monitoringEnabled,
    onRefresh: handleRefresh,
  };

  if (!monitoringEnabled) {
    return (
      <SettingsSectionShell {...shellProps}>
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
      </SettingsSectionShell>
    );
  }

  if (error) {
    return (
      <SettingsSectionShell {...shellProps}>
        <AppAlert severity="error">
          <AppAlertTitle>Monitoring settings unavailable</AppAlertTitle>
          {error.message}
        </AppAlert>
      </SettingsSectionShell>
    );
  }

  if (configSchemaError) {
    return (
      <SettingsSectionShell {...shellProps}>
        {renderAgentStatusCard()}
        <AppAlert severity="error">
          <AppAlertTitle>Monitoring config contract mismatch</AppAlertTitle>
          {configSchemaError} Update go-monitoring so its config API matches the
          LinuxIO monitoring settings contract.
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

  const ttlKeys = Object.keys(draft.cache_ttl).sort();
  const editableListeners = draft.listeners
    .map((listener, index) => ({ listener, index }))
    .filter(({ listener }) => listener.apis.includes("metrics"));

  return (
    <SettingsSectionShell {...shellProps}>
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

      {renderAgentStatusCard()}

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
          <SettingsGrid>
            {editableListeners.map(({ listener, index }) => (
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
            ))}
          </SettingsGrid>
        </SectionCard>
      ) : null}

      <SectionCard
        icon="mdi:timer-cog-outline"
        subtitle="Sampling cadence and history retention plugins"
        title="Collector"
      >
        <SettingsGrid>
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
            <AppTooltip title="How often SMART data is refreshed">
              <AppTextField
                disabled={busy}
                error={Boolean(errors.smart_refresh_interval)}
                fullWidth
                helperText={errors.smart_refresh_interval}
                label="SMART refresh"
                onChange={(event) =>
                  updateDraft("smart_refresh_interval", event.target.value)
                }
                size="small"
                value={draft.smart_refresh_interval}
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
          </>
        </SettingsGrid>
      </SectionCard>

      {ttlKeys.length > 0 ? (
        <SectionCard
          collapsible
          defaultCollapsed
          icon="mdi:cached"
          subtitle="How long live readings are cached per plugin"
          title="Cache TTLs"
        >
          <SettingsGrid minColumnWidth={140} rowGap={2.75}>
            {ttlKeys.map((key) => (
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
            ))}
          </SettingsGrid>
        </SectionCard>
      ) : null}

      <SettingsSaveFooter
        busy={busy}
        isDirty={isDirty}
        onReset={handleReset}
        onSave={handleSave}
        saveDisabled={hasErrors(errors)}
        saving={setConfigMutation.isPending}
      />
    </SettingsSectionShell>
  );
};

export default MonitoringSettingsSection;
