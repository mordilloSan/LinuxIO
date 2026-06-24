import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useMemo, useState } from "react";

import {
  CACHE_TTL_MS,
  linuxio,
  type DockerContainerAutoUpdateMode,
  type DockerContainerAutoUpdateOptions,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";
import { getMutationErrorMessage } from "@/utils/mutations";

const DEFAULT_OPTIONS: DockerContainerAutoUpdateOptions = {
  cleanup: false,
  container_names: [],
  enabled: false,
  mode: "update",
  time: "04:00",
};

interface ContainerAutoUpdateDialogProps {
  onClose: () => void;
  open: boolean;
  watchtowerEnabled: boolean;
  watchtowerReason?: string;
}

const normalizeOptions = (options: DockerContainerAutoUpdateOptions) => ({
  ...options,
  container_names: [...new Set(options.container_names)].sort(),
});

const optionKey = (options: DockerContainerAutoUpdateOptions) =>
  JSON.stringify(normalizeOptions(options));

const ContainerAutoUpdateDialog: React.FC<ContainerAutoUpdateDialogProps> = ({
  onClose,
  open,
  watchtowerEnabled,
  watchtowerReason,
}) => {
  const theme = useAppTheme();
  const toast = useScopedToast({ href: "/docker", label: "Open Docker" });
  const queryClient = useQueryClient();
  const [draftOverrides, setDraftOverrides] =
    useState<Partial<DockerContainerAutoUpdateOptions> | null>(null);
  const [containerNamesOverride, setContainerNamesOverride] = useState<
    string[] | null
  >(null);

  const autoUpdateQuery = linuxio.docker.get_container_auto_update.useQuery({
    enabled: open,
    staleTime: CACHE_TTL_MS.TWO_SECONDS,
  });
  const saveMutation = linuxio.docker.set_container_auto_update.useMutation({
    onSuccess: (state) => {
      toast.success("Container auto-update settings saved");
      setDraftOverrides(null);
      setContainerNamesOverride(null);
      queryClient.setQueryData(
        linuxio.docker.get_container_auto_update.queryKey(),
        state,
      );
      queryClient.invalidateQueries({
        queryKey: linuxio.docker.get_container_auto_update.queryKey(),
      });
    },
    onError: (err: Error) =>
      toast.error(
        getMutationErrorMessage(
          err,
          "Failed to save container auto-update settings",
        ),
      ),
  });

  const serverState = autoUpdateQuery.data;
  const baseOptions = serverState?.options ?? DEFAULT_OPTIONS;
  const selectedNames =
    containerNamesOverride ??
    baseOptions.container_names ??
    DEFAULT_OPTIONS.container_names;
  const currentOptions = useMemo<DockerContainerAutoUpdateOptions>(
    () => ({
      ...baseOptions,
      ...(draftOverrides ?? {}),
      container_names: selectedNames,
    }),
    [baseOptions, draftOverrides, selectedNames],
  );
  const dirty = optionKey(currentOptions) !== optionKey(baseOptions);
  const loading = autoUpdateQuery.isPending && !serverState;
  const saving = saveMutation.isPending;
  const unavailable =
    !watchtowerEnabled || !serverState?.available || !!autoUpdateQuery.error;
  const controlsDisabled = loading || saving || unavailable;
  const unavailableReason =
    autoUpdateQuery.error?.message ??
    serverState?.error ??
    watchtowerReason ??
    "Watchtower is unavailable.";

  const updateDraft = <K extends keyof DockerContainerAutoUpdateOptions>(
    key: K,
    value: DockerContainerAutoUpdateOptions[K],
  ) =>
    setDraftOverrides((prev) => ({
      ...(prev ?? {}),
      [key]: value,
    }));

  const removeSelectedName = (name: string) => {
    setContainerNamesOverride((prev) =>
      (prev ?? currentOptions.container_names).filter((item) => item !== name),
    );
  };

  const reset = () => {
    setDraftOverrides(null);
    setContainerNamesOverride(null);
  };

  const save = () => {
    saveMutation.mutate(currentOptions);
  };

  const missingNames = (serverState?.missing_container_names ?? []).filter(
    (name) => currentOptions.container_names.includes(name),
  );

  return (
    <GeneralDialog
      fullWidth
      maxWidth="md"
      onClose={() => !saving && onClose()}
      open={open}
      paperStyle={{ borderRadius: 8 }}
    >
      <AppDialogTitle
        style={{
          alignItems: "center",
          display: "flex",
          gap: theme.spacing(1),
          justifyContent: "space-between",
        }}
      >
        <span
          style={{
            alignItems: "center",
            display: "flex",
            gap: theme.spacing(1),
            minWidth: 0,
          }}
        >
          <Icon height={22} icon="mdi:timer-cog-outline" width={22} />
          <AppTypography component="span" variant="subtitle1">
            Container Auto-Update
          </AppTypography>
        </span>
        <AppActionIconButton
          ariaLabel="Close container auto-update settings"
          disabled={saving}
          icon="mdi:close"
          iconSize={20}
          label="Close"
          onClick={onClose}
        />
      </AppDialogTitle>

      <AppDialogContent
        style={{
          display: "grid",
          gap: theme.spacing(2),
          paddingTop: theme.spacing(1),
        }}
      >
        {unavailable && (
          <AppAlert severity="warning">
            <AppAlertTitle>Watchtower unavailable</AppAlertTitle>
            {unavailableReason}
          </AppAlert>
        )}

        <div
          style={{
            alignItems: "center",
            display: "flex",
            flexWrap: "wrap",
            gap: theme.spacing(2),
          }}
        >
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={currentOptions.enabled}
                disabled={controlsDisabled}
                onChange={(_, checked) => updateDraft("enabled", checked)}
              />
            }
            label="Scheduled auto-update"
          />
          <AppChip
            color={serverState?.timer_enabled ? "success" : "default"}
            label={
              serverState?.timer_enabled ? "Timer enabled" : "Timer disabled"
            }
            size="small"
            variant="soft"
          />
          {serverState?.timer_active && (
            <AppChip
              color="info"
              label="Timer active"
              size="small"
              variant="soft"
            />
          )}
        </div>

        <div
          style={{
            display: "grid",
            gap: theme.spacing(2),
            gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))",
          }}
        >
          <AppSelect
            disabled={controlsDisabled}
            label="Mode"
            onChange={(event) =>
              updateDraft(
                "mode",
                event.target.value as DockerContainerAutoUpdateMode,
              )
            }
            size="small"
            value={currentOptions.mode}
          >
            <option value="update">Update automatically</option>
            <option value="check_only">Check only</option>
          </AppSelect>
          <AppTextField
            disabled={controlsDisabled}
            fullWidth
            label="Daily time"
            onChange={(event) => updateDraft("time", event.target.value)}
            shrinkLabel
            size="small"
            type="time"
            value={currentOptions.time}
          />
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={currentOptions.cleanup}
                disabled={controlsDisabled}
                onChange={(_, checked) => updateDraft("cleanup", checked)}
              />
            }
            label="Cleanup old images"
            style={{ alignSelf: "end", minHeight: 40 }}
          />
        </div>

        {missingNames.length > 0 && (
          <div
            style={{
              borderTop: `1px solid ${alpha(theme.palette.divider, 0.7)}`,
              display: "grid",
              gap: theme.spacing(1.25),
              paddingTop: theme.spacing(2),
            }}
          >
            <AppTypography color="text.secondary" variant="subtitle2">
              Missing Containers
            </AppTypography>
            <div
              style={{
                display: "flex",
                flexWrap: "wrap",
                gap: theme.spacing(1),
              }}
            >
              {missingNames.map((name) => (
                <AppChip
                  color="warning"
                  disabled={controlsDisabled}
                  key={name}
                  label={name}
                  onDelete={() => removeSelectedName(name)}
                  size="small"
                  title="Selected container is not currently present"
                  variant="soft"
                />
              ))}
            </div>
          </div>
        )}
      </AppDialogContent>

      <AppDialogActions>
        <AppButton disabled={saving} onClick={onClose}>
          Close
        </AppButton>
        <AppButton
          disabled={!dirty || saving}
          onClick={reset}
          variant="outlined"
        >
          Reset
        </AppButton>
        <AppButton
          disabled={controlsDisabled || !dirty}
          onClick={save}
          variant="contained"
        >
          {saving ? "Saving..." : "Save"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default ContainerAutoUpdateDialog;
