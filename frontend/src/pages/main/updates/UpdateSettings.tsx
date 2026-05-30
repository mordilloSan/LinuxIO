import React, { useMemo, useState } from "react";

import {
  type AutoUpdateFrequency,
  type AutoUpdateOptions,
  type AutoUpdateRebootPolicy,
  type AutoUpdateScope,
  type AutoUpdateState,
  jobSnapshotResult,
  linuxio,
} from "@/api";
import PageLoader from "@/components/loaders/PageLoader";
import AppButton from "@/components/ui/AppButton";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";
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
  } = linuxio.updates.get_auto_updates.useQuery({
    enabled,
  });
  const toast = useScopedToast({ href: "/updates", label: "Open updates" });
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
    linuxio.updates.set_auto_updates.useMutation({
      onSuccess: () => {
        reset();
        refetch();
        toast.success("Automatic Updates Settings saved");
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to save auto-update settings"),
        );
      },
    });
  const { mutate: applyOfflineUpdates, isPending: isApplyingOffline } =
    linuxio.updates.apply_offline_updates.useMutation({
      onSuccess: (result) => {
        const updateResult = jobSnapshotResult(result);
        if (updateResult?.status && updateResult.status !== "ok") {
          const errMsg =
            updateResult.error || "Failed to schedule offline update";
          if (
            errMsg.includes("no updates available") ||
            errMsg.includes("Prepared update not found")
          ) {
            toast.info("No updates available to schedule");
          }
          return;
        }
        toast.success("Offline update scheduled for next reboot");
      },
      onError: (error: Error) => {
        const errMsg = error?.message || String(error);
        if (
          errMsg.includes("no updates available") ||
          errMsg.includes("Prepared update not found")
        ) {
          toast.info("No updates available to schedule");
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
    setAutoUpdates(payload);
  };
  const applyOffline = () => {
    applyOfflineUpdates();
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
            disabled={saving}
            onChange={(e) =>
              setDraftOverrides((prev) => ({
                ...(prev ?? {}),
                enabled: e.target.checked,
              }))
            }
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
          <AppTypography gutterBottom variant="subtitle2">
            Frequency
          </AppTypography>
          <AppSelect
            disabled={saving}
            onChange={(e) =>
              setDraftOverrides((prev) => ({
                ...(prev ?? {}),
                frequency: e.target.value as AutoUpdateFrequency,
              }))
            }
            size="small"
            value={currentOptions.frequency}
          >
            <option value="hourly">Hourly</option>
            <option value="daily">Daily</option>
            <option value="weekly">Weekly</option>
          </AppSelect>
        </div>

        <div>
          <AppTypography gutterBottom variant="subtitle2">
            Scope
          </AppTypography>
          <AppSelect
            disabled={saving}
            onChange={(e) =>
              setDraftOverrides((prev) => ({
                ...(prev ?? {}),
                scope: e.target.value as AutoUpdateScope,
              }))
            }
            size="small"
            value={currentOptions.scope}
          >
            <option value="security">Security only</option>
            <option value="updates">Security + updates</option>
            <option value="all">All (incl. extras)</option>
          </AppSelect>
        </div>

        <div>
          <AppTypography gutterBottom variant="subtitle2">
            Reboot policy
          </AppTypography>
          <AppSelect
            disabled={saving}
            onChange={(e) =>
              setDraftOverrides((prev) => ({
                ...(prev ?? {}),
                reboot_policy: e.target.value as AutoUpdateRebootPolicy,
              }))
            }
            size="small"
            value={currentOptions.reboot_policy}
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
              disabled={saving}
              onChange={(e) =>
                setDraftOverrides((prev) => ({
                  ...(prev ?? {}),
                  download_only: e.target.checked,
                }))
              }
            />
          }
          label="Download only (no auto-install)"
        />
      </div>

      <div>
        <AppTypography gutterBottom variant="subtitle2">
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
            disabled={saving}
            onChange={(e) => setExcludeInputOverride(e.target.value)}
            placeholder="e.g. linux-headers-*, docker-ce"
            size="small"
            style={{ width: "100%", maxWidth: 600 }}
            value={currentExcludeInput}
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
          disabled={saving || !dirty}
          onClick={save}
          variant="contained"
        >
          Save
        </AppButton>
        <AppButton disabled={saving || !dirty} onClick={reset} variant="text">
          Cancel
        </AppButton>
        <div
          style={{
            flexGrow: 1,
          }}
        />
        <AppButton disabled={saving} onClick={applyOffline} variant="contained">
          Apply at next reboot (offline)
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
