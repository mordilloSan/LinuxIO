import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useEffect, useState } from "react";
import { toast } from "sonner";

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
  open: boolean;
  onClose: () => void;
}

const SetDateTimeDialog: React.FC<Props> = ({ open, onClose }) => {
  const theme = useAppTheme();
  const queryClient = useQueryClient();

  const { data: timezones = [] } = linuxio.system.get_timezones.useQuery({
    enabled: open,
    staleTime: 60 * 60 * 1000,
  });
  const { data: currentTimezone } = linuxio.dbus.get_timezone.useQuery({
    enabled: open,
  });
  const { data: ntpStatus } = linuxio.dbus.get_ntp_status.useQuery({
    enabled: open,
  });
  const { data: ntpServers } = linuxio.dbus.get_ntp_servers.useQuery({
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

  useEffect(() => {
    if (currentTimezone !== undefined) {
      setTimezone(currentTimezone);
      setOriginalTimezone(currentTimezone);
    }
  }, [currentTimezone]);

  useEffect(() => {
    if (ntpStatus !== undefined && ntpServers !== undefined) {
      const mode = deriveMode(ntpStatus, ntpServers);
      setTimeMode(mode);
      setOriginalMode(mode);
      setCustomServers(ntpServers.length > 0 ? [...ntpServers] : [""]);
      setOriginalServers([...ntpServers]);
    }
  }, [ntpStatus, ntpServers]);

  useEffect(() => {
    if (serverTime) setManualTime(toDatetimeLocal(serverTime));
  }, [serverTime]);

  const { mutateAsync: setTz } = linuxio.dbus.set_timezone.useMutation({
    onError: (e: Error) =>
      toast.error(getMutationErrorMessage(e, "Failed to set timezone")),
  });
  const { mutateAsync: setNtp } = linuxio.dbus.set_ntp.useMutation({
    onError: (e: Error) =>
      toast.error(getMutationErrorMessage(e, "Failed to update NTP")),
  });
  const { mutateAsync: setServers } = linuxio.dbus.set_ntp_servers.useMutation({
    onError: (e: Error) =>
      toast.error(getMutationErrorMessage(e, "Failed to set NTP servers")),
  });
  const { mutateAsync: setTime } = linuxio.dbus.set_server_time.useMutation({
    onError: (e: Error) =>
      toast.error(getMutationErrorMessage(e, "Failed to set server time")),
  });

  const [isPending, setIsPending] = useState(false);

  const handleSave = async () => {
    setIsPending(true);
    try {
      if (timezone && timezone !== originalTimezone) {
        await setTz([timezone]);
      }
      if (
        timeMode !== originalMode ||
        JSON.stringify(customServers) !== JSON.stringify(originalServers)
      ) {
        if (timeMode === "manual") {
          await setNtp(["false"]);
        } else {
          await setNtp(["true"]);
          const servers =
            timeMode === "custom"
              ? customServers.map((s) => s.trim()).filter(Boolean)
              : [];
          await setServers(servers);
        }
      }
      if (timeMode === "manual" && manualTime) {
        await setTime([new Date(manualTime).toISOString()]);
      }
      toast.success("Date/time settings updated");
      queryClient.invalidateQueries({
        queryKey: linuxio.system.get_server_time.queryKey(),
      });
      queryClient.invalidateQueries({
        queryKey: linuxio.dbus.get_ntp_status.queryKey(),
      });
      queryClient.invalidateQueries({
        queryKey: linuxio.dbus.get_ntp_servers.queryKey(),
      });
      queryClient.invalidateQueries({
        queryKey: linuxio.dbus.get_timezone.queryKey(),
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
    <GeneralDialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <AppDialogTitle>Date &amp; Time Settings</AppDialogTitle>
      <AppDialogContent>
        {/* Sentinel captures initial focus so autocomplete dropdowns don't open automatically */}
        <span
          tabIndex={-1}
          style={{ outline: "none", display: "block", height: 0 }}
        />
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: theme.spacing(4),
          }}
        >
          <AppAutocomplete
            label="Time zone"
            size="small"
            options={timezones}
            value={timezone}
            onChange={(v) => setTimezone(v)}
            fullWidth
            filterOptions={(opts, state) => {
              if (!state.inputValue || state.inputValue === timezone)
                return opts;
              const q = state.inputValue.toLowerCase();
              return opts.filter((o) => o.toLowerCase().includes(q));
            }}
          />

          <AppAutocomplete
            label="Set time"
            size="small"
            options={TIME_MODE_OPTIONS}
            value={TIME_MODE_LABELS[timeMode]}
            onChange={handleModeChange}
            fullWidth
            filterOptions={(opts, state) => {
              if (
                !state.inputValue ||
                state.inputValue === TIME_MODE_LABELS[timeMode]
              )
                return opts;
              const q = state.inputValue.toLowerCase();
              return opts.filter((o) => o.toLowerCase().includes(q));
            }}
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
                    label={i === 0 ? "NTP server" : undefined}
                    placeholder="e.g. time.cloudflare.com"
                    value={server}
                    onChange={(e) => updateServer(i, e.target.value)}
                    fullWidth
                    variant="outlined"
                    size="small"
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
              label="Date &amp; Time"
              type="datetime-local"
              fullWidth
              variant="outlined"
              size="small"
              value={manualTime}
              onChange={(e) => setManualTime(e.target.value)}
            />
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          onClick={handleSave}
          disabled={isPending}
          variant="contained"
        >
          Save
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default SetDateTimeDialog;
