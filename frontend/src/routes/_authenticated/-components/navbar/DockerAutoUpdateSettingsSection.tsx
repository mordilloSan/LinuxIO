import { Icon } from "@iconify/react";
import { useMemo, useState } from "react";

import {
  type DockerContainerAutoUpdateMode,
  type DockerContainerAutoUpdateOptions,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useAppTheme } from "@/theme";
import { GAP_MD } from "@/theme/constants";

import {
  DEFAULT_AUTO_UPDATE_OPTIONS,
  optionsKey,
} from "./dockerAutoUpdateState";
import type { DockerAutoUpdateController } from "./useDockerAutoUpdateState";

interface DockerAutoUpdateSettingsSectionProps {
  autoUpdate: DockerAutoUpdateController;
  dockerUpdatesEnabled?: boolean;
  dockerUpdatesReason?: string;
}

const DockerAutoUpdateSettingsSection = ({
  autoUpdate,
  dockerUpdatesEnabled = true,
  dockerUpdatesReason,
}: DockerAutoUpdateSettingsSectionProps) => {
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

  const reset = () => {
    setDraftOverrides(null);
    setContainerNamesOverride(null);
  };

  // The settings writer updates the cache immediately so the draft can clear,
  // then rolls back and reports the error if the save fails.
  const save = () => {
    autoUpdate.saveOptions(currentOptions);
    reset();
  };

  const missingNames = (serverState?.missing_container_names ?? []).filter(
    (name) => currentOptions.container_names.includes(name),
  );
  const blockedReasons = currentOptions.container_names.flatMap((name) => {
    if (currentOptions.mode !== "update") return [];
    const target = serverState?.containers.find(
      (container) => container.name === name,
    );
    const eligibility = autoUpdate.targetEligibility.get(name);
    if (eligibility?.mutationAllowed === false) {
      return [
        {
          name,
          reason:
            eligibility.mutationReason ??
            "This container cannot be updated automatically.",
        },
      ];
    }
    if (target?.state === "exited" && !currentOptions.update_stopped) {
      return [
        {
          name,
          reason:
            "Enable stopped-container updates or remove this container from the automatic-update targets.",
        },
      ];
    }
    return [];
  });
  const hasBlockedNames = blockedReasons.length > 0;
  const selectableNames = useMemo(() => {
    const names = new Set<string>([
      ...currentOptions.container_names,
      ...(serverState?.containers ?? []).map((container) => container.name),
    ]);
    return Array.from(names).filter((name) => {
      if (currentOptions.container_names.includes(name)) return true;
      return autoUpdate.targetEligibility.get(name)?.mutationAllowed !== false;
    });
  }, [
    autoUpdate.targetEligibility,
    currentOptions.container_names,
    serverState?.containers,
  ]);

  return (
    <div
      aria-busy={saving}
      style={{ display: "grid", gap: theme.spacing(1.5) }}
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
          Remove these containers from Automatic-update targets before saving
          “Update automatically”, or switch the mode to “Check only”.
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
                  label={name}
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
              ? currentOptions.include_stopped
                ? "Enabled — running and stopped containers are checked on schedule"
                : "Enabled — all running containers are checked on schedule"
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
        aria-label="Stopped container update settings"
        style={{
          display: loading ? "none" : "grid",
          gap: theme.spacing(2),
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
            icon="mdi:power-sleep"
            width={19}
          />
          <div>
            <AppTypography fontWeight={600} variant="body2">
              Stopped containers
            </AppTypography>
            <AppTypography color="text.secondary" variant="caption">
              Control how scheduled checks and updates handle stopped containers
            </AppTypography>
          </div>
        </div>

        <div style={{ display: "grid", gap: theme.spacing(1.5) }}>
          <div style={{ alignItems: "center", display: "flex", gap: GAP_MD }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <AppTypography variant="body2">
                Check stopped containers
              </AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                Include stopped containers in scheduled availability checks
              </AppTypography>
            </div>
            <AppSwitch
              aria-label="Check stopped containers"
              checked={currentOptions.include_stopped}
              disabled={controlsDisabled}
              onChange={(_, checked) => updateDraft("include_stopped", checked)}
              size="small"
            />
          </div>

          <div style={{ alignItems: "center", display: "flex", gap: GAP_MD }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <AppTypography variant="body2">
                Update stopped containers
              </AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                Recreate selected standalone containers and keep them stopped
              </AppTypography>
            </div>
            <AppSwitch
              aria-label="Update stopped containers"
              checked={currentOptions.update_stopped}
              disabled={controlsDisabled}
              onChange={(_, checked) => {
                updateDraft("update_stopped", checked);
                if (!checked) updateDraft("revive_stopped", false);
              }}
              size="small"
            />
          </div>

          <div style={{ alignItems: "center", display: "flex", gap: GAP_MD }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <AppTypography variant="body2">Start after update</AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                Start and verify stopped containers after recreating them
              </AppTypography>
            </div>
            <AppSwitch
              aria-label="Start stopped containers after update"
              checked={currentOptions.revive_stopped}
              disabled={controlsDisabled || !currentOptions.update_stopped}
              onChange={(_, checked) => updateDraft("revive_stopped", checked)}
              size="small"
            />
          </div>
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

        <AppAutocomplete
          disabled={controlsDisabled}
          fullWidth
          helperText={
            currentOptions.mode === "update"
              ? "Select the containers to update after each scheduled check."
              : currentOptions.include_stopped
                ? "Saved for Update automatically; scheduled checks include running and stopped containers."
                : "Saved for Update automatically; scheduled checks include all running containers."
          }
          label="Automatic-update targets"
          maxListHeight={260}
          multiple
          noOptionsText="No eligible containers"
          onChange={(names) => setContainerNamesOverride(names)}
          options={selectableNames}
          placeholder="Select containers"
          shrinkLabel
          size="small"
          value={currentOptions.container_names}
        />

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
                key={name}
                label={name}
                size="small"
                title="Selected container is not currently present"
                variant="soft"
              />
            ))}
          </div>
        </FrostedCard>
      )}
      <div
        style={{
          display: "flex",
          gap: theme.spacing(1.5),
          justifyContent: "flex-end",
          paddingTop: theme.spacing(0.5),
        }}
      >
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
      </div>
    </div>
  );
};

export default DockerAutoUpdateSettingsSection;
