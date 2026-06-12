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
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppChip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
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

const includesSearch = (value: string, search: string) =>
  value.toLowerCase().includes(search.toLowerCase());

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
  const [selectionOverride, setSelectionOverride] = useState<string[] | null>(
    null,
  );
  const [search, setSearch] = useState("");

  const autoUpdateQuery = linuxio.docker.get_container_auto_update.useQuery({
    enabled: open,
    staleTime: CACHE_TTL_MS.TWO_SECONDS,
  });
  const saveMutation = linuxio.docker.set_container_auto_update.useMutation({
    onSuccess: (state) => {
      toast.success("Container auto-update settings saved");
      setDraftOverrides(null);
      setSelectionOverride(null);
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
    selectionOverride ??
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

  const selectedSet = useMemo(
    () => new Set(currentOptions.container_names),
    [currentOptions.container_names],
  );
  const containers = useMemo(
    () => serverState?.containers ?? [],
    [serverState?.containers],
  );
  const visibleContainers = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return containers;
    return containers.filter(
      (container) =>
        includesSearch(container.name, query) ||
        includesSearch(container.image, query) ||
        includesSearch(container.state, query),
    );
  }, [containers, search]);

  const updateDraft = <K extends keyof DockerContainerAutoUpdateOptions>(
    key: K,
    value: DockerContainerAutoUpdateOptions[K],
  ) =>
    setDraftOverrides((prev) => ({
      ...(prev ?? {}),
      [key]: value,
    }));

  const toggleContainer = (name: string) => {
    setSelectionOverride((prev) => {
      const next = new Set(prev ?? currentOptions.container_names);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return Array.from(next);
    });
  };

  const removeSelectedName = (name: string) => {
    setSelectionOverride((prev) =>
      (prev ?? currentOptions.container_names).filter((item) => item !== name),
    );
  };

  const reset = () => {
    setDraftOverrides(null);
    setSelectionOverride(null);
    setSearch("");
  };

  const save = () => {
    saveMutation.mutate(currentOptions);
  };

  const selectedCount = currentOptions.container_names.length;
  const selectedLabel =
    selectedCount === 1
      ? "1 selected container"
      : `${selectedCount} selected containers`;
  const missingNames = serverState?.missing_container_names ?? [];

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
        <AppTooltip title="Close">
          <AppIconButton
            aria-label="Close container auto-update settings"
            disabled={saving}
            onClick={onClose}
            size="small"
          >
            <Icon height={20} icon="mdi:close" width={20} />
          </AppIconButton>
        </AppTooltip>
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

        <div
          style={{
            borderTop: `1px solid ${alpha(theme.palette.divider, 0.7)}`,
            display: "grid",
            gap: theme.spacing(1.5),
            paddingTop: theme.spacing(2),
          }}
        >
          <div
            style={{
              alignItems: "center",
              display: "flex",
              flexWrap: "wrap",
              gap: theme.spacing(1),
              justifyContent: "space-between",
            }}
          >
            <AppTypography color="text.secondary" variant="subtitle2">
              Containers
            </AppTypography>
            <AppChip label={selectedLabel} size="small" variant="outlined" />
          </div>

          <AppTextField
            disabled={loading}
            fullWidth
            label="Search containers"
            onChange={(event) => setSearch(event.target.value)}
            size="small"
            startAdornment={<Icon height={18} icon="mdi:magnify" width={18} />}
            value={search}
          />

          {missingNames.length > 0 && (
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
          )}

          <div
            className="custom-scrollbar"
            style={{
              border: `1px solid ${alpha(theme.palette.divider, 0.7)}`,
              borderRadius: 8,
              maxHeight: 300,
              minHeight: 140,
              overflowY: "auto",
            }}
          >
            {loading ? (
              <AppTypography
                color="text.secondary"
                style={{ padding: theme.spacing(2) }}
                variant="body2"
              >
                Loading containers...
              </AppTypography>
            ) : visibleContainers.length === 0 ? (
              <AppTypography
                color="text.secondary"
                style={{ padding: theme.spacing(2) }}
                variant="body2"
              >
                No containers found
              </AppTypography>
            ) : (
              visibleContainers.map((container) => (
                <label
                  key={container.id}
                  style={{
                    alignItems: "center",
                    borderBottom: `1px solid ${alpha(theme.palette.divider, 0.5)}`,
                    cursor: controlsDisabled ? "default" : "pointer",
                    display: "grid",
                    gap: theme.spacing(1.5),
                    gridTemplateColumns: "auto minmax(0, 1fr) auto",
                    minHeight: 52,
                    padding: `${theme.spacing(1)} ${theme.spacing(1.5)}`,
                  }}
                >
                  <AppCheckbox
                    checked={selectedSet.has(container.name)}
                    disabled={controlsDisabled}
                    onChange={() => toggleContainer(container.name)}
                    size="small"
                  />
                  <span style={{ minWidth: 0 }}>
                    <AppTypography
                      component="span"
                      fontWeight={600}
                      noWrap
                      variant="body2"
                    >
                      {container.name}
                    </AppTypography>
                    <AppTypography
                      color="text.secondary"
                      component="span"
                      noWrap
                      style={{ display: "block" }}
                      variant="caption"
                    >
                      {container.image}
                    </AppTypography>
                  </span>
                  <AppChip
                    label={container.state}
                    size="small"
                    variant="outlined"
                  />
                </label>
              ))
            )}
          </div>
        </div>
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
