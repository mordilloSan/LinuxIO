import { Icon } from "@iconify/react";
import { useMemo, useState } from "react";

import {
  type DockerContainerAutoUpdateMode,
  type DockerContainerAutoUpdateOptions,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useAppTheme } from "@/theme";

import { DEFAULT_AUTO_UPDATE_OPTIONS, optionsKey } from "./containerAutoUpdate";
import type { ContainerAutoUpdateController } from "./useContainerAutoUpdateState";

interface ContainerAutoUpdateDialogProps {
  autoUpdate: ContainerAutoUpdateController;
  onClose: () => void;
  open: boolean;
  dockerUpdatesEnabled: boolean;
  dockerUpdatesReason?: string;
}

const ContainerAutoUpdateDialog = ({
  autoUpdate,
  onClose,
  open,
  dockerUpdatesEnabled,
  dockerUpdatesReason,
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
    !loading &&
    (!dockerUpdatesEnabled ||
      !serverState?.available ||
      !!autoUpdate.queryError);
  const controlsDisabled = loading || saving || unavailable;
  const unavailableReason =
    autoUpdate.queryError ??
    serverState?.error ??
    dockerUpdatesReason ??
    "Docker updates are unavailable.";

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
  const blockedNames = currentOptions.container_names.filter((name) => {
    if (currentOptions.mode !== "update") return false;
    return autoUpdate.targetEligibility.get(name)?.mutationAllowed === false;
  });
  const blockedReasons = blockedNames.map((name) => ({
    name,
    reason:
      autoUpdate.targetEligibility.get(name)?.mutationReason ??
      "This container cannot be updated automatically.",
  }));
  const hasBlockedNames = blockedReasons.length > 0;

  return (
    <GeneralDialog
      aria-busy={saving}
      disableEscapeKeyDown={saving}
      fullWidth
      maxWidth="md"
      onClose={() => !saving && onClose()}
      open={open}
      paperStyle={{ borderRadius: 12 }}
    >
      <AppDialogTitle
        style={{
          backgroundColor: theme.palette.background.paper,
          borderBottom: `1px solid ${theme.palette.divider}`,
          padding: "10px 12px",
        }}
      >
        <div
          style={{
            alignItems: "center",
            display: "flex",
            gap: theme.spacing(1),
          }}
        >
          <div
            style={{
              alignItems: "center",
              background: theme.palette.action.hover,
              borderRadius: 9,
              color: theme.palette.primary.main,
              display: "inline-flex",
              flexShrink: 0,
              height: 36,
              justifyContent: "center",
              width: 36,
            }}
          >
            <Icon height={22} icon="mdi:timer-cog-outline" width={22} />
          </div>
          <div
            style={{
              alignSelf: "stretch",
              display: "flex",
              flexDirection: "column",
              flexGrow: 1,
              justifyContent: "center",
              minWidth: 0,
            }}
          >
            <AppTypography
              fontWeight={600}
              style={{ lineHeight: 1.25 }}
              variant="subtitle1"
            >
              Container Auto-Update
            </AppTypography>
            <AppTypography
              color="text.secondary"
              style={{ lineHeight: 1.35 }}
              variant="caption"
            >
              Schedule image checks and automatic container updates
            </AppTypography>
          </div>
          <AppIconButton
            aria-label="Close container auto-update settings"
            disabled={saving}
            onClick={onClose}
            size="small"
          >
            <Icon height={18} icon="mdi:close" width={18} />
          </AppIconButton>
        </div>
      </AppDialogTitle>

      <AppDialogContent
        style={{
          display: "grid",
          gap: theme.spacing(1.5),
          padding: 10,
        }}
      >
        {loading && (
          <div
            style={{
              display: "grid",
              minHeight: 160,
              placeItems: "center",
            }}
          >
            <ComponentLoader />
          </div>
        )}
        {unavailable && (
          <AppAlert severity="warning">
            <AppAlertTitle>Docker updates unavailable</AppAlertTitle>
            {unavailableReason}
          </AppAlert>
        )}
        {!loading && hasBlockedNames && (
          <AppAlert severity="warning">
            <AppAlertTitle>
              Selected containers cannot be updated automatically
            </AppAlertTitle>
            Remove these containers before saving “Update automatically”, or
            switch the mode to “Check only”.
            <div
              style={{
                display: "flex",
                flexWrap: "wrap",
                gap: theme.spacing(1),
                marginTop: theme.spacing(1),
              }}
            >
              {blockedReasons.map(({ name, reason }) => (
                <div
                  key={name}
                  style={{
                    alignItems: "center",
                    display: "flex",
                    flexWrap: "wrap",
                    gap: theme.spacing(1),
                  }}
                >
                  <AppChip
                    color="warning"
                    disabled={controlsDisabled}
                    label={name}
                    onDelete={() => removeSelectedName(name)}
                    size="small"
                    variant="soft"
                  />
                  <AppTypography color="text.secondary" variant="body2">
                    {reason}
                  </AppTypography>
                </div>
              ))}
            </div>
          </AppAlert>
        )}

        <FrostedCard
          aria-label="Scheduled update checks control"
          style={{
            alignItems: "center",
            display: loading ? "none" : "flex",
            flexWrap: "wrap",
            gap: theme.spacing(1.5),
            justifyContent: "space-between",
            minHeight: 66,
            padding: 14,
          }}
        >
          <div style={{ flex: 1, minWidth: 220 }}>
            <div
              style={{
                alignItems: "center",
                display: "flex",
                gap: theme.spacing(0.75),
              }}
            >
              <StatusDot
                color={
                  currentOptions.enabled
                    ? theme.palette.success.main
                    : theme.palette.text.disabled
                }
                size={8}
              />
              <AppTypography
                component="h3"
                fontWeight={600}
                style={{ lineHeight: 1.25 }}
                variant="body2"
              >
                Scheduled update checks
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
              {currentOptions.enabled
                ? "Enabled — all running containers are checked on schedule"
                : "Paused — scheduled update checks are disabled"}
            </AppTypography>
          </div>
          <div
            style={{
              alignItems: "center",
              display: "flex",
              flexWrap: "wrap",
              gap: theme.spacing(1),
            }}
          >
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
            <AppSwitch
              aria-label="Enable scheduled update checks"
              checked={currentOptions.enabled}
              disabled={controlsDisabled}
              onChange={(_, checked) => updateDraft("enabled", checked)}
            />
          </div>
        </FrostedCard>

        <FrostedCard
          aria-label="Edit container update policy"
          style={{
            display: loading ? "none" : "grid",
            gap: theme.spacing(3),
            padding: 14,
          }}
        >
          <div
            style={{
              alignItems: "center",
              display: "flex",
              gap: theme.spacing(1),
            }}
          >
            <Icon
              color={theme.palette.primary.main}
              height={19}
              icon="mdi:tune-variant"
              width={19}
            />
            <div style={{ flex: 1, minWidth: 0 }}>
              <AppTypography
                component="h3"
                fontWeight={600}
                style={{ lineHeight: 1.25 }}
                variant="body2"
              >
                Update policy
              </AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                Choose whether selected containers are updated after each check
              </AppTypography>
            </div>
          </div>

          <div
            style={{
              display: "grid",
              gap: theme.spacing(2),
              gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))",
            }}
          >
            <AppSelect
              disabled={controlsDisabled}
              fullWidth
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
          </div>
        </FrostedCard>

        <FrostedCard
          aria-label="Cleanup old images setting"
          style={{
            alignItems: "center",
            display: loading ? "none" : "flex",
            gap: theme.spacing(1),
            minHeight: 66,
            padding: 14,
          }}
        >
          <Icon
            color={theme.palette.primary.main}
            height={19}
            icon="mdi:image-remove-outline"
            width={19}
          />
          <div style={{ flex: 1, minWidth: 0 }}>
            <AppTypography fontWeight={600} variant="body2">
              Cleanup old images
            </AppTypography>
            <AppTypography
              color="text.secondary"
              style={{ display: "block" }}
              variant="caption"
            >
              Remove superseded images after successful updates
            </AppTypography>
          </div>
          <AppSwitch
            aria-label="Cleanup old images"
            checked={currentOptions.cleanup}
            disabled={controlsDisabled}
            onChange={(_, checked) => updateDraft("cleanup", checked)}
            size="small"
          />
        </FrostedCard>

        {!loading && missingNames.length > 0 && (
          <FrostedCard
            style={{
              display: "grid",
              gap: theme.spacing(1.25),
              padding: 14,
            }}
          >
            <AppTypography fontWeight={600} variant="body2">
              Missing containers
            </AppTypography>
            <AppTypography color="text.secondary" variant="caption">
              These selected containers are not currently present.
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
          </FrostedCard>
        )}
      </AppDialogContent>

      <AppDialogActions
        style={{
          backgroundColor: theme.palette.background.paper,
          padding: 8,
        }}
      >
        <AppButton color="inherit" disabled={saving} onClick={onClose}>
          Close
        </AppButton>
        <AppButton disabled={!dirty || saving} onClick={reset}>
          Reset
        </AppButton>
        <AppButton
          disabled={controlsDisabled || !dirty || hasBlockedNames}
          onClick={save}
          startIcon={
            <Icon height={17} icon="mdi:content-save-outline" width={17} />
          }
          variant="contained"
        >
          {saving ? "Saving..." : "Save"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default ContainerAutoUpdateDialog;
