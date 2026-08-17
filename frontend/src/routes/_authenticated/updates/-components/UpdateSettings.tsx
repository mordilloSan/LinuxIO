import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import {
  type AutoUpdateFrequency,
  type AutoUpdateOptions,
  type AutoUpdateRebootPolicy,
  type AutoUpdateScope,
  type AutoUpdateState,
  type Timer,
  linuxio,
  useCallMutation,
} from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppButton from "@/components/ui/AppButton";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useScopedToast } from "@/hooks/useScopedToast";
import { StatusMetric } from "@/routes/_authenticated/-components/navbar/SettingsSectionPrimitives";
import { useAppTheme } from "@/theme";

const UPDATES_TOAST_META = {
  label: "Open updates",
  to: "/updates",
} as const;

const normalizeState = (s: AutoUpdateState): AutoUpdateState => ({
  ...s,
  options: {
    ...s.options,
    exclude_packages: Array.isArray(s.options.exclude_packages)
      ? s.options.exclude_packages
      : [],
  },
});

interface ManagedTimer {
  allowedActive: boolean;
  label: string;
  name: string;
  required: boolean;
  timer?: Timer;
}

const autoUpdateTimerDefinitions = (
  state: AutoUpdateState,
): Omit<ManagedTimer, "timer">[] => {
  switch (state.backend) {
    case "apt-unattended":
      return [
        {
          allowedActive: true,
          label: "Download schedule",
          name: "apt-daily.timer",
          required: state.options.enabled,
        },
        {
          allowedActive: state.options.enabled && !state.options.download_only,
          label: "Install schedule",
          name: "apt-daily-upgrade.timer",
          required: state.options.enabled && !state.options.download_only,
        },
      ];
    case "mintupdate-automation":
      return [
        {
          allowedActive: state.options.enabled,
          label: "Update schedule",
          name: "mintupdate-automation-upgrade.timer",
          required: state.options.enabled,
        },
      ];
    case "dnf-automatic":
      return [
        {
          allowedActive: state.options.enabled,
          label: "Update schedule",
          name: "dnf-automatic.timer",
          required: state.options.enabled,
        },
      ];
    case "dnf5-automatic":
      return [
        {
          allowedActive: state.options.enabled,
          label: "Update schedule",
          name: "dnf5-automatic.timer",
          required: state.options.enabled,
        },
      ];
    default:
      return [];
  }
};

const managedTimers = (
  state: AutoUpdateState,
  timers: Timer[],
): ManagedTimer[] =>
  autoUpdateTimerDefinitions(state).map((definition) => ({
    ...definition,
    timer: timers.find((timer) => timer.name === definition.name),
  }));

const timerIsActive = (timer?: Timer) => timer?.active_state === "active";

const formatTimerDate = (usec?: number) => {
  if (!usec || !Number.isFinite(usec)) return "Not scheduled";
  const date = new Date(usec / 1000);
  return Number.isNaN(date.getTime()) ? "Not scheduled" : date.toLocaleString();
};

const frequencyLabels: Record<AutoUpdateFrequency, string> = {
  hourly: "Hourly",
  daily: "Daily",
  weekly: "Weekly",
};

const scopeLabels: Record<AutoUpdateScope, string> = {
  security: "Security only",
  updates: "Security + updates",
  all: "All enabled repositories",
};

const rebootLabels: Record<AutoUpdateRebootPolicy, string> = {
  never: "Never reboot",
  if_needed: "Reboot if needed",
  always: "Always reboot",
  schedule: "Scheduled reboot",
};

const backendLabels: Record<AutoUpdateState["backend"], string> = {
  "apt-unattended": "APT unattended upgrades",
  "dnf-automatic": "DNF Automatic",
  "dnf5-automatic": "DNF5 Automatic",
  "mintupdate-automation": "Mint Update Manager",
};

export const useUpdateSettingsState = (enabled = true) => {
  const { data: rawServerState, isLoading: loading } = useQuery({
    ...linuxio.updates.get_auto_updates,
    enabled,
  });
  const {
    data: timers = [],
    isError: runtimeError,
    isLoading: runtimeLoading,
  } = useQuery({
    ...linuxio.systemd.list_timers,
    enabled,
    refetchInterval: enabled ? 5000 : false,
  });
  const toast = useScopedToast(UPDATES_TOAST_META);
  const serverState = useMemo(
    () => (rawServerState ? normalizeState(rawServerState) : null),
    [rawServerState],
  );
  const [draftOverrides, setDraftOverrides] =
    useState<Partial<AutoUpdateOptions> | null>(null);
  const [excludeInputOverride, setExcludeInputOverride] = useState<
    string | null
  >(null);
  const currentOptions = useMemo(() => {
    if (!serverState) return null;
    return {
      ...serverState.options,
      ...draftOverrides,
    };
  }, [serverState, draftOverrides]);
  const currentExcludeInput = useMemo(() => {
    if (excludeInputOverride !== null) return excludeInputOverride;
    return serverState?.options.exclude_packages.join(", ") ?? "";
  }, [serverState, excludeInputOverride]);
  const reset = () => {
    setDraftOverrides(null);
    setExcludeInputOverride(null);
  };
  const { mutate: setAutoUpdates, isPending: isSettingAutoUpdates } =
    useCallMutation(linuxio.updates.set_auto_updates, {
      success: () => {
        reset();
        toast.success("Automatic Updates Settings saved");
      },
      error: "Failed to save auto-update settings",
      toast: UPDATES_TOAST_META,
    });
  const saving = isSettingAutoUpdates;
  const dirty = useMemo(() => {
    if (!serverState || !currentOptions) return false;
    const draftWithExcludes: AutoUpdateOptions = {
      ...currentOptions,
      exclude_packages: currentExcludeInput
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean),
    };
    return (
      JSON.stringify(serverState.options) !== JSON.stringify(draftWithExcludes)
    );
  }, [serverState, currentExcludeInput, currentOptions]);
  const save = () => {
    if (!currentOptions) return;
    const payload: AutoUpdateOptions = {
      ...currentOptions,
      exclude_packages: currentExcludeInput
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean),
    };
    setAutoUpdates(payload);
  };
  return {
    loading,
    runtimeError,
    runtimeLoading,
    serverState,
    timers,
    currentOptions,
    currentExcludeInput,
    saving,
    dirty,
    setDraftOverrides,
    setExcludeInputOverride,
    reset,
    save,
  };
};

interface AutoUpdateRuntimeProps {
  runtimeError: boolean;
  runtimeLoading: boolean;
  serverState: AutoUpdateState;
  timers: Timer[];
}

const AptServiceNote = () => {
  const theme = useAppTheme();

  return (
    <div
      aria-label="About unattended-upgrades.service"
      style={{
        background: theme.palette.action.hover,
        borderLeft: `3px solid ${theme.palette.primary.main}`,
        borderRadius: 6,
        marginTop: theme.spacing(2),
        padding: theme.spacing(1, 1.25),
      }}
    >
      <AppTypography fontWeight={600} variant="caption">
        About unattended-upgrades.service
      </AppTypography>
      <AppTypography
        color="text.secondary"
        style={{ display: "block", marginTop: 2 }}
        variant="caption"
      >
        This service coordinates shutdown; the timers above schedule updates.
        Its PyGIDeprecationWarning is an upstream package warning, not an update
        failure.
      </AppTypography>
    </div>
  );
};

const AutoUpdateRuntime = ({
  runtimeError,
  runtimeLoading,
  serverState,
  timers,
}: AutoUpdateRuntimeProps) => {
  const theme = useAppTheme();
  const units = managedTimers(serverState, timers);
  const requiredUnits = units.filter(({ required }) => required);
  const unexpectedActiveUnits = units.filter(
    ({ allowedActive, timer }) => !allowedActive && timerIsActive(timer),
  );
  const schedulerHealthy =
    serverState.options.enabled &&
    requiredUnits.length > 0 &&
    requiredUnits.every(({ timer }) => timerIsActive(timer)) &&
    unexpectedActiveUnits.length === 0;
  const schedulerStopped =
    !serverState.options.enabled && unexpectedActiveUnits.length === 0;
  const schedulerLabel = runtimeLoading
    ? "Checking"
    : runtimeError || units.length === 0
      ? "Unavailable"
      : schedulerHealthy
        ? "Operational"
        : schedulerStopped
          ? "Disabled"
          : "Needs attention";
  const schedulerColor = schedulerHealthy
    ? theme.palette.success.main
    : schedulerStopped || runtimeLoading
      ? theme.palette.text.disabled
      : theme.palette.warning.main;
  const nextRun = requiredUnits
    .map(({ timer }) => timer?.next_elapse_usec ?? 0)
    .filter((value) => value > 0)
    .sort((a, b) => a - b)[0];

  return (
    <FrostedCard
      aria-label="Automatic update runtime status"
      style={{
        padding: 14,
      }}
    >
      <CardIconHeader
        headingVariant="section"
        icon={<Icon height={22} icon="mdi:calendar-clock-outline" width={22} />}
        iconTint
        right={
          <div
            style={{
              alignItems: "center",
              display: "flex",
              gap: theme.spacing(0.75),
            }}
          >
            <StatusDot color={schedulerColor} size={9} />
            <AppTypography
              color="text.secondary"
              fontWeight={600}
              variant="caption"
            >
              {schedulerLabel}
            </AppTypography>
          </div>
        }
        style={{ marginBottom: theme.spacing(2) }}
        subtitle="Live systemd timer state"
        title="Scheduler status"
      />

      {runtimeError ? (
        <AppTypography color="text.secondary" variant="body2">
          LinuxIO could not read the systemd timer state. The saved
          configuration is still shown below.
        </AppTypography>
      ) : units.length > 0 ? (
        <div
          style={{
            display: "grid",
            gap: theme.spacing(2),
            gridTemplateColumns: "repeat(auto-fit, minmax(170px, 1fr))",
          }}
        >
          {units.map(({ label, name, required, timer }) => {
            const active = timerIsActive(timer);
            return (
              <StatusMetric
                detail={name}
                key={name}
                label={label}
                monoDetail
                statusColor={
                  active
                    ? theme.palette.success.main
                    : theme.palette.text.disabled
                }
                value={`${active ? "Active" : "Inactive"}${!required ? " · not required" : ""}`}
                variant="compact"
              />
            );
          })}
          <StatusMetric
            label="Next scheduled run"
            value={
              serverState.options.enabled
                ? formatTimerDate(nextRun)
                : "Not scheduled"
            }
            variant="compact"
          />
        </div>
      ) : (
        <AppTypography color="text.secondary" variant="body2">
          This backend does not expose a managed systemd timer.
        </AppTypography>
      )}
      {serverState.backend === "apt-unattended" ? <AptServiceNote /> : null}
    </FrostedCard>
  );
};

const SavedConfiguration = ({
  dirty,
  state,
}: {
  dirty: boolean;
  state: AutoUpdateState;
}) => {
  const theme = useAppTheme();
  const values = [
    {
      label: "Provider",
      value: backendLabels[state.backend],
    },
    {
      label: "Status",
      value: state.options.enabled ? "Enabled" : "Disabled",
      statusColor: state.options.enabled
        ? theme.palette.success.main
        : theme.palette.text.disabled,
    },
    {
      label: "Schedule",
      value: frequencyLabels[state.options.frequency],
    },
    {
      label: "Update scope",
      value: scopeLabels[state.options.scope],
    },
    {
      label: "Install mode",
      value: state.options.download_only
        ? "Download only"
        : "Download and install",
    },
    {
      label: "Reboots",
      value: rebootLabels[state.options.reboot_policy],
    },
    {
      label: "Package exclusions",
      value: state.options.exclude_packages.length
        ? state.options.exclude_packages.join(", ")
        : "None",
    },
  ];

  return (
    <FrostedCard
      aria-label="Saved automatic update configuration"
      style={{
        padding: 14,
      }}
    >
      <CardIconHeader
        headingVariant="section"
        icon={<Icon height={22} icon="mdi:file-check-outline" width={22} />}
        iconTint
        right={
          dirty ? (
            <div
              style={{
                alignItems: "center",
                display: "flex",
                gap: theme.spacing(0.75),
              }}
            >
              <StatusDot color={theme.palette.warning.main} size={8} />
              <AppTypography color="warning" fontWeight={600} variant="caption">
                Unsaved edits
              </AppTypography>
            </div>
          ) : (
            <StatusDot
              color={theme.palette.success.main}
              size={9}
              tooltip="Configuration loaded"
            />
          )
        }
        style={{ marginBottom: theme.spacing(2) }}
        subtitle="Settings currently applied on this server"
        title="Applied configuration"
      />
      <div
        style={{
          display: "grid",
          gap: theme.spacing(1.5),
          gridTemplateColumns: "repeat(auto-fit, minmax(170px, 1fr))",
        }}
      >
        {values.map(({ label, statusColor, value }) => (
          <StatusMetric
            key={label}
            label={label}
            statusColor={statusColor}
            value={value}
            variant="compact"
          />
        ))}
      </div>
    </FrostedCard>
  );
};

const AutomaticUpdatesControl = ({
  checked,
  disabled,
  onChange,
}: {
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}) => {
  const theme = useAppTheme();
  const statusColor = checked
    ? theme.palette.success.main
    : theme.palette.text.disabled;

  return (
    <FrostedCard
      aria-label="Automatic updates master control"
      style={{
        alignItems: "center",
        display: "flex",
        gap: theme.spacing(1.5),
        justifyContent: "space-between",
        minHeight: 66,
        padding: 14,
      }}
    >
      <div style={{ minWidth: 0 }}>
        <div
          style={{
            alignItems: "center",
            display: "flex",
            gap: theme.spacing(0.75),
          }}
        >
          <StatusDot color={statusColor} size={8} />
          <AppTypography
            component="h3"
            fontWeight={600}
            style={{ lineHeight: 1.25 }}
            variant="body2"
          >
            Automatic updates
          </AppTypography>
        </div>
        <AppTypography
          color="text.secondary"
          style={{
            display: "block",
            lineHeight: 1.35,
            marginLeft: theme.spacing(1.75),
            marginTop: 3,
          }}
          variant="caption"
        >
          {checked
            ? "Enabled — updates follow the schedule and policy below"
            : "Paused — automatic update schedules are disabled"}
        </AppTypography>
      </div>
      <AppSwitch
        aria-label="Enable automatic updates"
        checked={checked}
        disabled={disabled}
        onChange={(_, nextChecked) => onChange(nextChecked)}
      />
    </FrostedCard>
  );
};

interface UpdateSettingsProps {
  disablePadding?: boolean;
  state: ReturnType<typeof useUpdateSettingsState>;
}
const UpdateSettings = ({
  disablePadding = false,
  state,
}: UpdateSettingsProps) => {
  const theme = useAppTheme();
  const {
    loading,
    runtimeError,
    runtimeLoading,
    serverState,
    timers,
    currentOptions,
    currentExcludeInput,
    saving,
    dirty,
    setDraftOverrides,
    setExcludeInputOverride,
    reset,
    save,
  } = state;
  if (loading || !serverState || !currentOptions) {
    return <ComponentLoader />;
  }
  return (
    <div
      style={{
        padding: disablePadding ? 0 : 12,
        display: "grid",
        gap: theme.spacing(1.5),
      }}
    >
      <AutoUpdateRuntime
        runtimeError={runtimeError}
        runtimeLoading={runtimeLoading}
        serverState={serverState}
        timers={timers}
      />

      <SavedConfiguration dirty={dirty} state={serverState} />

      <AutomaticUpdatesControl
        checked={currentOptions.enabled}
        disabled={saving || !serverState.can_configure}
        onChange={(enabled) =>
          setDraftOverrides((previous) => ({
            ...previous,
            enabled,
          }))
        }
      />

      <FrostedCard
        aria-label="Edit automatic update policy"
        style={{
          padding: 14,
        }}
      >
        <CardIconHeader
          headingVariant="section"
          icon={<Icon height={22} icon="mdi:tune-variant" width={22} />}
          iconTint
          style={{ marginBottom: theme.spacing(2) }}
          subtitle="Choose what Linux installs and when it runs"
          title="Update policy"
        />

        <div
          style={{
            alignItems: "end",
            display: "grid",
            gap: theme.spacing(2),
            gridTemplateColumns: "repeat(auto-fit, minmax(155px, 1fr))",
            marginTop: theme.spacing(1),
          }}
        >
          <AppSelect
            disabled={
              saving ||
              !serverState.can_configure ||
              serverState.support.frequencies.length <= 1
            }
            fullWidth
            label="Frequency"
            onChange={(event) =>
              setDraftOverrides((previous) => ({
                ...previous,
                frequency: event.target.value as AutoUpdateFrequency,
              }))
            }
            size="small"
            value={currentOptions.frequency}
          >
            {serverState.support.frequencies.map((frequency) => (
              <option key={frequency} value={frequency}>
                {frequencyLabels[frequency]}
              </option>
            ))}
          </AppSelect>

          <AppSelect
            disabled={
              saving ||
              !serverState.can_configure ||
              serverState.support.scopes.length <= 1
            }
            fullWidth
            label="Update scope"
            onChange={(event) =>
              setDraftOverrides((previous) => ({
                ...previous,
                scope: event.target.value as AutoUpdateScope,
              }))
            }
            size="small"
            value={currentOptions.scope}
          >
            {serverState.support.scopes.map((scope) => (
              <option key={scope} value={scope}>
                {scopeLabels[scope]}
              </option>
            ))}
          </AppSelect>

          <AppSelect
            disabled={
              saving ||
              !serverState.can_configure ||
              serverState.support.reboot_policies.length <= 1
            }
            fullWidth
            label="Reboot policy"
            onChange={(event) =>
              setDraftOverrides((previous) => ({
                ...previous,
                reboot_policy: event.target.value as AutoUpdateRebootPolicy,
              }))
            }
            size="small"
            value={currentOptions.reboot_policy}
          >
            {serverState.support.reboot_policies.map((policy) => (
              <option key={policy} value={policy}>
                {rebootLabels[policy]}
              </option>
            ))}
          </AppSelect>

          <div
            style={{
              alignItems: "center",
              background: theme.palette.action.hover,
              borderRadius: 9,
              display: "flex",
              gap: theme.spacing(1),
              minHeight: 40,
              padding: `0 ${theme.spacing(1.25)}`,
            }}
          >
            <Icon
              color={theme.palette.primary.main}
              height={19}
              icon="mdi:download-outline"
              width={19}
            />
            <div style={{ flex: 1, minWidth: 0 }}>
              <AppTypography fontWeight={600} variant="caption">
                Download only
              </AppTypography>
              <AppTypography
                color="text.secondary"
                noWrap
                style={{ display: "block" }}
                variant="caption"
              >
                Skip automatic installation
              </AppTypography>
            </div>
            <AppSwitch
              aria-label="Download only without automatic installation"
              checked={currentOptions.download_only}
              disabled={
                saving ||
                !serverState.can_configure ||
                !serverState.support.download_only
              }
              onChange={(event) =>
                setDraftOverrides((previous) => ({
                  ...previous,
                  download_only: event.target.checked,
                }))
              }
              size="small"
            />
          </div>
        </div>

        <div
          style={{
            marginTop: theme.spacing(2.5),
          }}
        >
          <AppTextField
            disabled={
              saving ||
              !serverState.can_configure ||
              !serverState.support.exclude_packages
            }
            fullWidth
            id="automatic-update-exclusions"
            label="Package exclusions"
            onChange={(event) => setExcludeInputOverride(event.target.value)}
            placeholder="e.g. linux-headers-*, docker-ce"
            shrinkLabel
            size="small"
            value={currentExcludeInput}
          />
          <AppTypography
            color="text.secondary"
            style={{ display: "block", marginTop: 4 }}
            variant="caption"
          >
            Optional comma-separated package names or patterns.
          </AppTypography>
        </div>
      </FrostedCard>

      <div
        style={{
          alignItems: "center",
          borderTop: `1px solid ${theme.palette.divider}`,
          display: "flex",
          flexWrap: "wrap",
          gap: theme.spacing(1),
          justifyContent: "flex-end",
          paddingTop: theme.spacing(1.5),
        }}
      >
        <AppButton
          disabled={saving || !dirty || !serverState.can_configure}
          onClick={reset}
          variant="text"
        >
          Reset
        </AppButton>
        <AppButton
          disabled={saving || !dirty || !serverState.can_configure}
          onClick={save}
          startIcon={<Icon height={17} icon="mdi:content-save" width={17} />}
          variant="contained"
        >
          {saving ? "Saving..." : "Save changes"}
        </AppButton>
        {serverState.notes?.length ? (
          <AppTypography
            color="text.secondary"
            style={{
              width: "100%",
            }}
            variant="body2"
          >
            {serverState.notes.join(" • ")}
          </AppTypography>
        ) : null}
      </div>
    </div>
  );
};
export default UpdateSettings;
