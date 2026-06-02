import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";

import { linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
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
import { useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";

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

const SetDateTimeDialog: React.FC<Props> = ({ open, onClose }) => {
  const theme = useAppTheme();
  const toast = useScopedToast({ href: "/", label: "Open dashboard" });
  const queryClient = useQueryClient();

  const { data: timezones = [] } = linuxio.system.get_timezones.useQuery({
    enabled: open,
    staleTime: 60 * 60 * 1000,
  });
  const { data: currentTimezone } = linuxio.datetime.get_timezone.useQuery({
    enabled: open,
  });
  const { data: ntpStatus } = linuxio.datetime.get_ntp_status.useQuery({
    enabled: open,
  });
  const { data: ntpServers } = linuxio.datetime.get_ntp_servers.useQuery({
    enabled: open,
  });
  const { data: serverTime } = linuxio.system.get_server_time.useQuery({
    enabled: open,
    staleTime: 0,
  });

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

  const { mutateAsync: setTz } = linuxio.datetime.set_timezone.useMutation({
    onError: (e: Error) =>
      toast.error(getMutationErrorMessage(e, "Failed to set timezone")),
  });
  const { mutateAsync: setNtp } = linuxio.datetime.set_ntp.useMutation({
    onError: (e: Error) =>
      toast.error(getMutationErrorMessage(e, "Failed to update NTP")),
  });
  const { mutateAsync: setServers } =
    linuxio.datetime.set_ntp_servers.useMutation({
      onError: (e: Error) =>
        toast.error(getMutationErrorMessage(e, "Failed to set NTP servers")),
    });
  const { mutateAsync: setTime } = linuxio.datetime.set_server_time.useMutation(
    {
      onError: (e: Error) =>
        toast.error(getMutationErrorMessage(e, "Failed to set server time")),
    },
  );

  const [isPending, setIsPending] = useState(false);

  const handleSave = async () => {
    setIsPending(true);
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
      queryClient.invalidateQueries({
        queryKey: linuxio.system.get_server_time.queryKey(),
      });
      queryClient.invalidateQueries({
        queryKey: linuxio.datetime.get_ntp_status.queryKey(),
      });
      queryClient.invalidateQueries({
        queryKey: linuxio.datetime.get_ntp_servers.queryKey(),
      });
      queryClient.invalidateQueries({
        queryKey: linuxio.datetime.get_timezone.queryKey(),
      });
      onClose();
    } catch {
      // individual errors already toasted by mutation onError
    } finally {
      setIsPending(false);
    }
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
        {/* Sentinel captures initial focus so autocomplete dropdowns don't open automatically */}
        <span
          style={{ outline: "none", display: "block", height: 0 }}
          tabIndex={-1}
        />
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: theme.spacing(4),
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
            options={timezones}
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
                    gap: theme.spacing(1),
                    marginBottom: theme.spacing(1),
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
                    onClick={addServer}
                    style={{ marginTop: i === 0 ? theme.spacing(1) : 0 }}
                    title="Add server"
                  >
                    <Icon icon="mdi:plus" width={18} />
                  </AppIconButton>
                  <AppIconButton
                    onClick={() => removeServer(i)}
                    style={{ marginTop: i === 0 ? theme.spacing(1) : 0 }}
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
          disabled={isPending}
          onClick={handleSave}
          variant="contained"
        >
          Save
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default SetDateTimeDialog;
