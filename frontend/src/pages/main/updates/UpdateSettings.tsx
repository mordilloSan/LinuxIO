import React, { useMemo, useState } from "react";
import { toast } from "sonner";

import {
  linuxio,
  type AutoUpdateFrequency,
  type AutoUpdateOptions,
  type AutoUpdateRebootPolicy,
  type AutoUpdateScope,
  type AutoUpdateState,
} from "@/api";
import PageLoader from "@/components/loaders/PageLoader";
import AppButton from "@/components/ui/AppButton";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";
const updatesToastMeta = {
  meta: {
    href: "/updates",
    label: "Open updates",
  },
};
const normalizeState = (s: AutoUpdateState): AutoUpdateState => ({
  ...s,
  options: {
    ...s.options,
    exclude_packages: Array.isArray(s.options.exclude_packages)
      ? s.options.exclude_packages
      : [],
  },
});
export const useUpdateSettingsState = (enabled = true) => {
  const {
    data: rawServerState,
    isPending: loading,
    refetch,
  } = linuxio.dbus.get_auto_updates.useQuery({
    enabled,
  });
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
    linuxio.dbus.set_auto_updates.useMutation({
      onSuccess: () => {
        reset();
        refetch();
        toast.success("Automatic Updates Settings saved", updatesToastMeta);
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to save auto-update settings"),
        );
      },
    });
  const { mutate: applyOfflineUpdates, isPending: isApplyingOffline } =
    linuxio.dbus.apply_offline_updates.useMutation({
      onSuccess: (result) => {
        if (result?.status && result.status !== "ok") {
          const errMsg = result.error || "Failed to schedule offline update";
          if (
            errMsg.includes("no updates available") ||
            errMsg.includes("Prepared update not found")
          ) {
            toast.info("No updates available to schedule", updatesToastMeta);
          }
          return;
        }
        toast.success(
          "Offline update scheduled for next reboot",
          updatesToastMeta,
        );
      },
      onError: (error: Error) => {
        const errMsg = error?.message || String(error);
        if (
          errMsg.includes("no updates available") ||
          errMsg.includes("Prepared update not found")
        ) {
          toast.info("No updates available to schedule", updatesToastMeta);
        } else {
          toast.error(
            getMutationErrorMessage(error, "Failed to schedule offline update"),
          );
        }
      },
    });
  const saving = isSettingAutoUpdates || isApplyingOffline;
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
    setAutoUpdates([payload]);
  };
  const applyOffline = () => {
    applyOfflineUpdates([]);
  };
  return {
    loading,
    serverState,
    currentOptions,
    currentExcludeInput,
    saving,
    dirty,
    setDraftOverrides,
    setExcludeInputOverride,
    reset,
    save,
    applyOffline,
  };
};
interface UpdateSettingsProps {
  disablePadding?: boolean;
  state: ReturnType<typeof useUpdateSettingsState>;
}
const UpdateSettings: React.FC<UpdateSettingsProps> = ({
  disablePadding = false,
  state,
}) => {
  const theme = useAppTheme();
  const {
    loading,
    serverState,
    currentOptions,
    currentExcludeInput,
    saving,
    dirty,
    setDraftOverrides,
    setExcludeInputOverride,
    reset,
    save,
    applyOffline,
  } = state;
  if (loading || !serverState || !currentOptions) {
    return <PageLoader />;
  }
  return (
    <div
      style={{
        padding: disablePadding ? 0 : 12,
        display: "grid",
        gap: 8,
      }}
    >
      <AppFormControlLabel
        control={
          <AppSwitch
            checked={currentOptions.enabled}
            onChange={(e) =>
              setDraftOverrides((prev) => ({
                ...(prev ?? {}),
                enabled: e.target.checked,
              }))
            }
            disabled={saving}
          />
        }
        label="Enable automatic updates"
      />

      <div
        style={{
          display: "flex",
          gap: theme.spacing(3),
          alignItems: "center",
          flexWrap: "wrap",
        }}
      >
        <div>
          <AppTypography variant="subtitle2" gutterBottom>
            Frequency
          </AppTypography>
          <AppSelect
            size="small"
            value={currentOptions.frequency}
            onChange={(e) =>
              setDraftOverrides((prev) => ({
                ...(prev ?? {}),
                frequency: e.target.value as AutoUpdateFrequency,
              }))
            }
            disabled={saving}
          >
            <option value="hourly">Hourly</option>
            <option value="daily">Daily</option>
            <option value="weekly">Weekly</option>
          </AppSelect>
        </div>

        <div>
          <AppTypography variant="subtitle2" gutterBottom>
            Scope
          </AppTypography>
          <AppSelect
            size="small"
            value={currentOptions.scope}
            onChange={(e) =>
              setDraftOverrides((prev) => ({
                ...(prev ?? {}),
                scope: e.target.value as AutoUpdateScope,
              }))
            }
            disabled={saving}
          >
            <option value="security">Security only</option>
            <option value="updates">Security + updates</option>
            <option value="all">All (incl. extras)</option>
          </AppSelect>
        </div>

        <div>
          <AppTypography variant="subtitle2" gutterBottom>
            Reboot policy
          </AppTypography>
          <AppSelect
            size="small"
            value={currentOptions.reboot_policy}
            onChange={(e) =>
              setDraftOverrides((prev) => ({
                ...(prev ?? {}),
                reboot_policy: e.target.value as AutoUpdateRebootPolicy,
              }))
            }
            disabled={saving}
          >
            <option value="never">Never</option>
            <option value="if_needed">If needed</option>
            <option value="always">Always</option>
          </AppSelect>
        </div>

        <AppFormControlLabel
          control={
            <AppSwitch
              checked={currentOptions.download_only}
              onChange={(e) =>
                setDraftOverrides((prev) => ({
                  ...(prev ?? {}),
                  download_only: e.target.checked,
                }))
              }
              disabled={saving}
            />
          }
          label="Download only (no auto-install)"
        />
      </div>

      <div>
        <AppTypography variant="subtitle2" gutterBottom>
          Exclude packages (comma-separated)
        </AppTypography>
        <div
          style={{
            display: "flex",
            gap: theme.spacing(1),
            alignItems: "center",
          }}
        >
          <AppTextField
            size="small"
            placeholder="e.g. linux-headers-*, docker-ce"
            value={currentExcludeInput}
            onChange={(e) => setExcludeInputOverride(e.target.value)}
            disabled={saving}
            style={{ width: "100%", maxWidth: 600 }}
          />
        </div>
      </div>

      <div
        style={{
          display: "flex",
          gap: 4,
          alignItems: "center",
          flexWrap: "wrap",
          marginTop: 4,
        }}
      >
        <AppButton
          variant="contained"
          onClick={save}
          disabled={saving || !dirty}
        >
          Save
        </AppButton>
        <AppButton variant="text" onClick={reset} disabled={saving || !dirty}>
          Cancel
        </AppButton>
        <div
          style={{
            flexGrow: 1,
          }}
        />
        <AppButton variant="contained" onClick={applyOffline} disabled={saving}>
          Apply at next reboot (offline)
        </AppButton>
        {serverState.notes?.length ? (
          <AppTypography
            variant="body2"
            color="text.secondary"
            style={{
              width: "100%",
            }}
          >
            {serverState.notes.join(" • ")}
          </AppTypography>
        ) : null}
      </div>
    </div>
  );
};
export default UpdateSettings;
