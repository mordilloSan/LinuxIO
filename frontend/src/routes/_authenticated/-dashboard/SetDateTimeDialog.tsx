import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio, useCallMutation } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert from "@/components/ui/AppAlert";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTextField from "@/components/ui/AppTextField";
import { useScopedToast } from "@/hooks/useScopedToast";

const DASHBOARD_TOAST_META = { label: "Open dashboard", to: "/" } as const;

type TimeMode = "auto" | "custom" | "manual";

const TIME_MODE_LABELS: Record<TimeMode, string> = {
  auto: "Automatically (NTP)",
  custom: "Automatically using specific NTP servers",
  manual: "Manually",
};
const TIME_MODE_OPTIONS = Object.values(TIME_MODE_LABELS);
const LABEL_TO_MODE = Object.fromEntries(
  Object.entries(TIME_MODE_LABELS).map(([k, v]) => [v, k as TimeMode]),
);

function deriveMode(ntpEnabled: boolean, servers: string[]): TimeMode {
  if (!ntpEnabled) return "manual";
  return servers.length > 0 ? "custom" : "auto";
}

function toDatetimeLocal(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}`
  );
}

interface Props {
  onClose: () => void;
  open: boolean;
}

const SetDateTimeDialog = ({ open, onClose }: Props) => {
  const toast = useScopedToast(DASHBOARD_TOAST_META);

  const {
    data: timezones,
    isLoading: timezonesLoading,
    isError: timezonesError,
  } = useQuery({
    ...linuxio.system.get_timezones,
    enabled: open,
    staleTime: 60 * 60 * 1000,
  });
  const {
    data: currentTimezone,
    isLoading: timezoneLoading,
    isError: timezoneError,
  } = useQuery({ ...linuxio.datetime.get_timezone, enabled: open });
  const {
    data: ntpStatus,
    isLoading: ntpStatusLoading,
    isError: ntpStatusError,
  } = useQuery({ ...linuxio.datetime.get_ntp_status, enabled: open });
  const {
    data: ntpServers,
    isLoading: ntpServersLoading,
    isError: ntpServersError,
  } = useQuery({ ...linuxio.datetime.get_ntp_servers, enabled: open });
  const {
    data: serverTime,
    isLoading: serverTimeLoading,
    isError: serverTimeError,
  } = useQuery({
    ...linuxio.system.get_server_time,
    enabled: open,
    staleTime: 0,
  });

  const settingsLoading =
    timezonesLoading ||
    timezoneLoading ||
    ntpStatusLoading ||
    ntpServersLoading ||
    serverTimeLoading;
  const settingsReady =
    timezones !== undefined &&
    currentTimezone !== undefined &&
    ntpStatus !== undefined &&
    ntpServers !== undefined &&
    serverTime !== undefined;
  const settingsError =
    !settingsReady &&
    (timezonesError ||
      timezoneError ||
      ntpStatusError ||
      ntpServersError ||
      serverTimeError);
  const showSettingsLoader =
    settingsLoading || (!settingsReady && !settingsError);

  const [timezone, setTimezone] = useState("");
  const [originalTimezone, setOriginalTimezone] = useState("");
  const [timeMode, setTimeMode] = useState<TimeMode>("auto");
  const [originalMode, setOriginalMode] = useState<TimeMode>("auto");
  const [customServers, setCustomServers] = useState<string[]>([""]);
  const [originalServers, setOriginalServers] = useState<string[]>([]);
  const [manualTime, setManualTime] = useState("");

  const [syncedTimezone, setSyncedTimezone] = useState<string | undefined>(
    undefined,
  );
  if (currentTimezone !== undefined && currentTimezone !== syncedTimezone) {
    setSyncedTimezone(currentTimezone);
    setTimezone(currentTimezone);
    setOriginalTimezone(currentTimezone);
  }

  const ntpKey =
    ntpStatus !== undefined && ntpServers !== undefined
      ? `${ntpStatus}:${ntpServers.join(",")}`
      : undefined;
  const [syncedNtpKey, setSyncedNtpKey] = useState<string | undefined>(
    undefined,
  );
  if (ntpKey !== undefined && ntpKey !== syncedNtpKey) {
    setSyncedNtpKey(ntpKey);
    const mode = deriveMode(ntpStatus!, ntpServers!);
    setTimeMode(mode);
    setOriginalMode(mode);
    setCustomServers(ntpServers!.length > 0 ? [...ntpServers!] : [""]);
    setOriginalServers([...ntpServers!]);
  }

  const [syncedServerTime, setSyncedServerTime] = useState<string | undefined>(
    undefined,
  );
  if (serverTime && serverTime !== syncedServerTime) {
    setSyncedServerTime(serverTime);
    setManualTime(toDatetimeLocal(serverTime));
  }

  const { mutateAsync: setTz } = useCallMutation(
    linuxio.datetime.set_timezone,
    {
      error: "Failed to set timezone",
      toast: DASHBOARD_TOAST_META,
    },
  );
  const { mutateAsync: setNtp } = useCallMutation(linuxio.datetime.set_ntp, {
    error: "Failed to update NTP",
    toast: DASHBOARD_TOAST_META,
  });
  const { mutateAsync: setServers } = useCallMutation(
    linuxio.datetime.set_ntp_servers,
    {
      error: "Failed to set NTP servers",
      toast: DASHBOARD_TOAST_META,
    },
  );
  const { mutateAsync: setTime } = useCallMutation(
    linuxio.datetime.set_server_time,
    {
      error: "Failed to set server time",
      toast: DASHBOARD_TOAST_META,
    },
  );

  const [isSaving, setIsSaving] = useState(false);

  const handleSave = async () => {
    if (!settingsReady) return;

    setIsSaving(true);
    const save = async () => {
      try {
        if (timezone && timezone !== originalTimezone) {
          await setTz({ timezone });
        }
        if (
          timeMode !== originalMode ||
          JSON.stringify(customServers) !== JSON.stringify(originalServers)
        ) {
          if (timeMode === "manual") {
            await setNtp({ enabled: "false" });
          } else {
            await setNtp({ enabled: "true" });
            const servers =
              timeMode === "custom"
                ? customServers.map((s) => s.trim()).filter(Boolean)
                : [];
            await setServers({ servers });
          }
        }
        if (timeMode === "manual" && manualTime) {
          await setTime({ isoTime: new Date(manualTime).toISOString() });
        }
        toast.success("Date/time settings updated");
        onClose();
      } catch {
        // individual errors already toasted by mutation onError
      }
    };
    await save().finally(() => setIsSaving(false));
  };

  const handleModeChange = (label: string) => {
    const mode = LABEL_TO_MODE[label];
    if (!mode) return;
    setTimeMode(mode);
    if (mode === "custom" && customServers.every((s) => !s.trim())) {
      setCustomServers([""]);
    }
  };

  const updateServer = (i: number, val: string) => {
    setCustomServers((prev) => prev.map((s, idx) => (idx === i ? val : s)));
  };
  const removeServer = (i: number) => {
    setCustomServers((prev) => {
      const next = prev.filter((_, idx) => idx !== i);
      return next.length === 0 ? [""] : next;
    });
  };
  const addServer = () => setCustomServers((prev) => [...prev, ""]);

  return (
    <GeneralDialog fullWidth maxWidth="xs" onClose={onClose} open={open}>
      <AppDialogTitle>Date &amp; Time Settings</AppDialogTitle>
      <AppDialogContent>
        {showSettingsLoader && <ComponentLoader />}
        {settingsError && (
          <AppAlert severity="error">
            Date and time settings could not be loaded.
          </AppAlert>
        )}
        {/* Sentinel captures initial focus so autocomplete dropdowns don't open automatically */}
        <span
          style={{ outline: "none", display: "block", height: 0 }}
          tabIndex={-1}
        />
        <div
          style={{
            display: showSettingsLoader || settingsError ? "none" : "flex",
            flexDirection: "column",
            gap: "var(--app-space-16)",
          }}
        >
          <AppAutocomplete
            filterOptions={(opts, state) => {
              if (!state.inputValue || state.inputValue === timezone)
                return opts;
              const q = state.inputValue.toLowerCase();
              return opts.filter((o) => o.toLowerCase().includes(q));
            }}
            fullWidth
            label="Time zone"
            onChange={(v) => setTimezone(v)}
            options={timezones ?? []}
            size="small"
            value={timezone}
          />

          <AppAutocomplete
            filterOptions={(opts, state) => {
              if (
                !state.inputValue ||
                state.inputValue === TIME_MODE_LABELS[timeMode]
              )
                return opts;
              const q = state.inputValue.toLowerCase();
              return opts.filter((o) => o.toLowerCase().includes(q));
            }}
            fullWidth
            label="Set time"
            onChange={handleModeChange}
            options={TIME_MODE_OPTIONS}
            size="small"
            value={TIME_MODE_LABELS[timeMode]}
          />

          {timeMode === "custom" && (
            <div>
              {customServers.map((server, i) => (
                <div
                  key={i}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "var(--app-space-4)",
                    marginBottom: "var(--app-space-4)",
                  }}
                >
                  <AppTextField
                    fullWidth
                    label={i === 0 ? "NTP server" : undefined}
                    onChange={(e) => updateServer(i, e.target.value)}
                    placeholder="e.g. time.cloudflare.com"
                    size="small"
                    value={server}
                    variant="outlined"
                  />
                  <AppIconButton
                    aria-label="Add NTP server"
                    onClick={addServer}
                    style={{ marginTop: i === 0 ? "var(--app-space-4)" : 0 }}
                    title="Add server"
                  >
                    <Icon icon="mdi:plus" width={18} />
                  </AppIconButton>
                  <AppIconButton
                    aria-label="Remove NTP server"
                    onClick={() => removeServer(i)}
                    style={{ marginTop: i === 0 ? "var(--app-space-4)" : 0 }}
                    title="Remove server"
                  >
                    <Icon icon="mdi:close" width={18} />
                  </AppIconButton>
                </div>
              ))}
            </div>
          )}

          {timeMode === "manual" && (
            <AppTextField
              fullWidth
              label="Date &amp; Time"
              onChange={(e) => setManualTime(e.target.value)}
              size="small"
              type="datetime-local"
              value={manualTime}
              variant="outlined"
            />
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          disabled={isSaving || showSettingsLoader || settingsError}
          onClick={handleSave}
          variant="contained"
        >
          {isSaving ? "Saving..." : "Save"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default SetDateTimeDialog;
