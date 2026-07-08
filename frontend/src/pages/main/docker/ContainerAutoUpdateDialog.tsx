import { Icon } from "@iconify/react";
import React, { useMemo, useState } from "react";

import { DEFAULT_AUTO_UPDATE_OPTIONS, optionsKey } from "./containerAutoUpdate";
import type { ContainerAutoUpdateController } from "./useContainerAutoUpdateState";

import {
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
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

interface ContainerAutoUpdateDialogProps {
  autoUpdate: ContainerAutoUpdateController;
  onClose: () => void;
  open: boolean;
  watchtowerEnabled: boolean;
  watchtowerReason?: string;
}

const ContainerAutoUpdateDialog = ({
  autoUpdate,
  onClose,
  open,
  watchtowerEnabled,
  watchtowerReason,
}: ContainerAutoUpdateDialogProps) => {
  const theme = useAppTheme();
  const [draftOverrides, setDraftOverrides] =
    useState<Partial<DockerContainerAutoUpdateOptions> | null>(null);
  const [containerNamesOverride, setContainerNamesOverride] = useState<
    string[] | null
  >(null);

  const serverState = autoUpdate.state;
  const baseOptions = serverState?.options ?? DEFAULT_AUTO_UPDATE_OPTIONS;
  const selectedNames =
    containerNamesOverride ??
    baseOptions.container_names ??
    DEFAULT_AUTO_UPDATE_OPTIONS.container_names;
  const currentOptions = useMemo<DockerContainerAutoUpdateOptions>(
    () => ({
      ...baseOptions,
      ...(draftOverrides ?? {}),
      container_names: selectedNames,
    }),
    [baseOptions, draftOverrides, selectedNames],
  );
  const dirty = optionsKey(currentOptions) !== optionsKey(baseOptions);
  const loading = autoUpdate.isPending && !serverState;
  const saving = autoUpdate.isSaving;
  const unavailable =
    !watchtowerEnabled || !serverState?.available || !!autoUpdate.queryError;
  const controlsDisabled = loading || saving || unavailable;
  const unavailableReason =
    autoUpdate.queryError ??
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

  // Optimistic like the per-container toggles: the shared writer updates the
  // cache immediately (so the drafts can clear now) and rolls back with an
  // error toast if the save fails.
  const save = () => {
    autoUpdate.saveOptions(currentOptions);
    reset();
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
