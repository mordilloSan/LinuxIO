import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useState } from "react";
import type { CSSProperties } from "react";

import PreflightSummary from "./PreflightSummary";
import {
  CLOUD_INIT_IMAGE_PRESETS,
  DEFAULT_MANAGED_CLOUD_PATH,
  DEFAULT_MANAGED_ISO_PATH,
  IMAGE_PRESETS,
  VM_TOAST,
  folderFromISOPathText,
  isISOPath,
  isMissingPathError,
  parentDirectory,
} from "./vmShared";
import type {
  ReadyImagePreset,
  VMCreateMode,
  VMDialogImagePresetID,
  VMDialogSourceType,
  VMPresetID,
} from "./vmShared";

import { linuxio } from "@/api";
import type { VMCreateProgress, VMCreateRequest } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTextField from "@/components/ui/AppTextField";
import PathPickerField from "@/components/ui/PathPickerField";
import { useScopedToast } from "@/hooks/useScopedToast";
import { type AppTheme, useAppMediaQuery, useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";
import { ensureTrailingSlash } from "@/utils/path";

const createModeStyle = (theme: AppTheme, isMobile: boolean): CSSProperties =>
  isMobile
    ? {
        display: "grid",
        gap: theme.spacing(2),
        gridTemplateColumns: "repeat(2, minmax(0, 1fr))",
        marginBottom: theme.spacing(3),
      }
    : {
        display: "inline-flex",
        gap: theme.spacing(2),
        marginBottom: theme.spacing(3),
      };

const presetGroupStyle = (
  theme: AppTheme,
  isMobile: boolean,
): CSSProperties => ({
  display: "grid",
  gap: theme.spacing(2),
  gridTemplateColumns: isMobile
    ? "1fr"
    : "repeat(auto-fit, minmax(180px, 1fr))",
  marginBottom: theme.spacing(4),
});

const presetButtonStyle: CSSProperties = {
  alignItems: "flex-start",
  flexDirection: "column",
  gap: 2,
  minHeight: 56,
  minWidth: 0,
  padding: "8px 10px",
};

const presetMetaStyle: CSSProperties = {
  color: "inherit",
  fontSize: "0.72rem",
  fontWeight: 400,
  lineHeight: 1.35,
  opacity: 0.74,
};

const formGridStyle = (theme: AppTheme, isMobile: boolean): CSSProperties => ({
  display: "grid",
  gap: theme.spacing(4),
  gridTemplateColumns: isMobile ? "1fr" : "repeat(2, minmax(0, 1fr))",
});

const wideGridItemStyle: CSSProperties = {
  gridColumn: "1 / -1",
};

const checkboxLineStyle = (theme: AppTheme): CSSProperties => ({
  alignItems: "center",
  display: "inline-flex",
  gap: theme.spacing(2),
  margin: theme.spacing(3.5, 0),
});

const managedPathsStyle = (theme: AppTheme): CSSProperties => ({
  display: "flex",
  flexWrap: "wrap",
  gap: theme.spacing(2),
  margin: theme.spacing(0.5, 0, 3),
});

const managedPathChipStyle = (theme: AppTheme): CSSProperties => ({
  alignItems: "center",
  border: `1px solid ${theme.palette.divider}`,
  borderRadius: 6,
  color: theme.palette.text.secondary,
  display: "inline-flex",
  gap: theme.spacing(2),
  minWidth: 0,
  padding: theme.spacing(1.5, 2),
});

const createProgressStyle = (theme: AppTheme): CSSProperties => ({
  border: `1px solid ${theme.palette.divider}`,
  borderRadius: 6,
  display: "flex",
  flexDirection: "column",
  gap: theme.spacing(2),
  marginBottom: theme.spacing(3),
  padding: theme.spacing(2.5),
});

const createProgressHeaderStyle = (theme: AppTheme): CSSProperties => ({
  alignItems: "center",
  display: "flex",
  gap: theme.spacing(3),
  justifyContent: "space-between",
  minWidth: 0,
});

const messageListStyle: CSSProperties = {
  margin: 0,
  paddingLeft: 18,
};

const wrappingCodeStyle: CSSProperties = {
  overflowWrap: "anywhere",
};

export default function CreateVMDialog({
  createProgress,
  isCreating,
  onClose,
  onCreate,
  open,
}: {
  createProgress: VMCreateProgress | null;
  isCreating: boolean;
  onClose: () => void;
  onCreate: (request: VMCreateRequest) => void;
  open: boolean;
}) {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const queryClient = useQueryClient();
  const toast = useScopedToast(VM_TOAST);
  const [name, setName] = useState("");
  const [vcpus, setVCPUs] = useState("2");
  const [memoryMB, setMemoryMB] = useState("4096");
  const [diskGB, setDiskGB] = useState("32");
  const [isoPath, setISOPath] = useState("");
  const [start, setStart] = useState(true);
  const [createMode, setCreateMode] = useState<VMCreateMode>("iso");
  const [selectedPreset, setSelectedPreset] = useState<VMPresetID>("custom");
  const [sourceType, setSourceType] = useState<VMDialogSourceType>("iso");
  const [imagePresetId, setImagePresetId] = useState<
    VMDialogImagePresetID | undefined
  >(undefined);
  const [cloudInitUsername, setCloudInitUsername] = useState("linuxio");
  const [cloudInitPassword, setCloudInitPassword] = useState("");
  const [cloudInitSSHKey, setCloudInitSSHKey] = useState("");
  const usesISO = sourceType === "iso";
  const usesCloudInit = Boolean(
    imagePresetId && CLOUD_INIT_IMAGE_PRESETS.has(imagePresetId),
  );
  const preflight = linuxio.virt.preflight.useQuery(
    {
      imagePresetId,
      isoPath: usesISO ? isoPath || undefined : undefined,
      sourceType,
    },
    { enabled: open, refetchInterval: open ? 5000 : false },
  );
  const createISOFolderMutation =
    linuxio.filebrowser.resource_post.useMutation();
  const managedISOPath =
    preflight.data?.managedPaths?.isos ?? DEFAULT_MANAGED_ISO_PATH;
  const managedCloudPath =
    preflight.data?.managedPaths?.cloudImages ?? DEFAULT_MANAGED_CLOUD_PATH;

  const applyPreset = (preset: ReadyImagePreset) => {
    setSelectedPreset(preset.id);
    setVCPUs(preset.vcpus);
    setMemoryMB(preset.memoryMB);
    setDiskGB(preset.diskGB);
    setStart(preset.start);
    setSourceType(preset.sourceType);
    setCreateMode("image");
    setImagePresetId(preset.imagePresetId);
  };

  const applyCreateMode = (mode: VMCreateMode) => {
    if (mode === "image") {
      applyPreset(IMAGE_PRESETS[0]);
      return;
    }
    setCreateMode("iso");
    setSelectedPreset("custom");
    setVCPUs("2");
    setMemoryMB("4096");
    setDiskGB("32");
    setStart(true);
    setSourceType("iso");
    setImagePresetId(undefined);
  };

  const markCustom = () => setSelectedPreset("custom");

  const parsedVCPUs = Number.parseInt(vcpus, 10);
  const parsedMemoryMB = Number.parseInt(memoryMB, 10);
  const parsedDiskGB = Number.parseInt(diskGB, 10);
  const activeImagePreset = IMAGE_PRESETS.find(
    (preset) => preset.imagePresetId === imagePresetId,
  );
  const minimumDiskGB = activeImagePreset?.minDiskGB ?? 1;
  const nameValid = /^[A-Za-z0-9_.-]+$/.test(name);
  const cloudInitUsernameValid = /^[a-z_][a-z0-9_-]{0,31}$/.test(
    cloudInitUsername,
  );
  const cloudInitAuthProvided =
    cloudInitPassword.trim().length > 0 || cloudInitSSHKey.trim().length > 0;
  const trimmedISOPath = isoPath.trim();
  const isoPathProvided = trimmedISOPath.length > 0;
  const isoPathHasISOExtension = !isoPathProvided || isISOPath(trimmedISOPath);
  const fieldsValid =
    nameValid &&
    parsedVCPUs > 0 &&
    parsedMemoryMB >= 256 &&
    parsedDiskGB >= minimumDiskGB &&
    (!usesISO || (isoPathProvided && isoPathHasISOExtension)) &&
    (!usesCloudInit || (cloudInitUsernameValid && cloudInitAuthProvided));
  const hasBlockingPreflightErrors = (preflight.data?.errors ?? []).length > 0;
  const isBusy = isCreating || createISOFolderMutation.isPending;
  const canSubmit = fieldsValid && !isBusy && !hasBlockingPreflightErrors;

  const ensureISOFolderExists = useCallback(async () => {
    if (!usesISO) return;
    const folder = folderFromISOPathText(isoPath);
    if (!folder || folder === "/") return;

    try {
      const stat = await linuxio.filebrowser.resource_stat(folder);
      if (stat.mode && !stat.mode.startsWith("d")) {
        toast.error(`${folder} exists but is not a directory.`);
      }
      return;
    } catch (error) {
      if (!isMissingPathError(error)) {
        toast.error(
          getMutationErrorMessage(error, "Failed to check ISO folder"),
        );
        return;
      }
    }

    try {
      await createISOFolderMutation.mutateAsync({
        path: ensureTrailingSlash(folder),
      });
      toast.success(`Created ISO folder ${folder}`);
      queryClient.invalidateQueries({
        queryKey: linuxio.filebrowser.resource_get.queryKey({
          path: ensureTrailingSlash(parentDirectory(folder) || "/"),
        }),
      });
      void preflight.refetch();
    } catch (error) {
      toast.error(
        getMutationErrorMessage(error, "Failed to create ISO folder"),
      );
    }
  }, [
    createISOFolderMutation,
    isoPath,
    preflight,
    queryClient,
    toast,
    usesISO,
  ]);

  const handleSubmit = (event: React.SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) return;
    const request: VMCreateRequest = {
      diskGB: parsedDiskGB,
      memoryMB: parsedMemoryMB,
      name: name.trim(),
      network: "default",
      sourceType,
      start,
      vcpus: parsedVCPUs,
    };
    if (usesISO) {
      request.isoPath = isoPath.trim();
    }
    if (imagePresetId) {
      request.imagePresetId = imagePresetId;
    }
    if (usesCloudInit) {
      request.cloudInitUsername = cloudInitUsername.trim();
      if (cloudInitPassword.trim()) {
        request.cloudInitPassword = cloudInitPassword.trim();
      }
      if (cloudInitSSHKey.trim()) {
        request.cloudInitSshKey = cloudInitSSHKey.trim();
      }
    }
    onCreate(request);
  };

  return (
    <GeneralDialog
      fullWidth
      maxWidth="md"
      onClose={() => {
        if (!isBusy) {
          onClose();
        }
      }}
      open={open}
    >
      <form onSubmit={handleSubmit}>
        <AppDialogTitle>Create VM</AppDialogTitle>
        <AppDialogContent>
          <div
            aria-label="VM source"
            role="tablist"
            style={createModeStyle(theme, isMobile)}
          >
            <AppButton
              aria-selected={createMode === "iso"}
              onClick={() => applyCreateMode("iso")}
              role="tab"
              size="small"
              variant={createMode === "iso" ? "contained" : "outlined"}
            >
              ISO installer
            </AppButton>
            <AppButton
              aria-selected={createMode === "image"}
              onClick={() => applyCreateMode("image")}
              role="tab"
              size="small"
              variant={createMode === "image" ? "contained" : "outlined"}
            >
              Ready image
            </AppButton>
          </div>
          {createMode === "image" ? (
            <div
              aria-label="VM preset"
              role="radiogroup"
              style={presetGroupStyle(theme, isMobile)}
            >
              {IMAGE_PRESETS.map((preset) => (
                <AppButton
                  aria-checked={selectedPreset === preset.id}
                  key={preset.id}
                  onClick={() => applyPreset(preset)}
                  role="radio"
                  size="small"
                  style={presetButtonStyle}
                  variant={
                    selectedPreset === preset.id ? "contained" : "outlined"
                  }
                >
                  <span>{preset.label}</span>
                  <small style={presetMetaStyle}>
                    {preset.vcpus} CPU /{" "}
                    {Number.parseInt(preset.memoryMB, 10) / 1024} GB /{" "}
                    {preset.diskGB} GB
                  </small>
                </AppButton>
              ))}
              {selectedPreset === "custom" && (
                <AppButton
                  aria-checked="true"
                  role="radio"
                  size="small"
                  style={presetButtonStyle}
                  variant="contained"
                >
                  <span>Custom</span>
                  <small style={presetMetaStyle}>Manual sizing</small>
                </AppButton>
              )}
            </div>
          ) : null}
          <div style={formGridStyle(theme, isMobile)}>
            <AppTextField
              autoFocus
              error={name.length > 0 && !nameValid}
              fullWidth
              helperText="Letters, numbers, dash, underscore, and dot"
              id="vm-create-name"
              label="Name"
              onChange={(event) => setName(event.target.value)}
              required
              value={name}
            />
            <AppTextField
              fullWidth
              id="vm-create-vcpus"
              label="vCPUs"
              onChange={(event) => {
                markCustom();
                setVCPUs(event.target.value);
              }}
              required
              type="number"
              value={vcpus}
            />
            <AppTextField
              fullWidth
              id="vm-create-memory"
              label="Memory MB"
              onChange={(event) => {
                markCustom();
                setMemoryMB(event.target.value);
              }}
              required
              type="number"
              value={memoryMB}
            />
            <AppTextField
              error={diskGB.length > 0 && parsedDiskGB < minimumDiskGB}
              fullWidth
              helperText={
                minimumDiskGB > 1 ? `Minimum ${minimumDiskGB} GB` : undefined
              }
              id="vm-create-disk"
              label="Disk GB"
              onChange={(event) => {
                markCustom();
                setDiskGB(event.target.value);
              }}
              required
              type="number"
              value={diskGB}
            />
            {usesISO ? (
              <PathPickerField
                browsePath={`${managedISOPath}/`}
                browseLabel="Browse ISO files"
                editable
                error={isoPathProvided && !isoPathHasISOExtension}
                fileFilter={isISOPath}
                helperText={
                  isoPathProvided && !isoPathHasISOExtension
                    ? "Select a regular .iso file; folders cannot be used as install media"
                    : undefined
                }
                id="vm-create-iso"
                includeFiles
                label="ISO path"
                onBlur={() => {
                  void ensureISOFolderExists();
                }}
                onBrowsePathChange={setISOPath}
                onChange={setISOPath}
                onPickerClose={() => {
                  void ensureISOFolderExists();
                }}
                placeholder={`${managedISOPath}/debian.iso`}
                selectableTypes={["file"]}
                style={wideGridItemStyle}
                required
                value={isoPath}
              />
            ) : null}
            {usesCloudInit ? (
              <>
                <AppTextField
                  error={
                    cloudInitUsername.length > 0 && !cloudInitUsernameValid
                  }
                  fullWidth
                  helperText="Lowercase letters, numbers, dash, and underscore"
                  id="vm-create-cloud-username"
                  label="Login username"
                  onChange={(event) => setCloudInitUsername(event.target.value)}
                  required
                  value={cloudInitUsername}
                />
                <AppTextField
                  fullWidth
                  id="vm-create-cloud-password"
                  label="Login password"
                  onChange={(event) => setCloudInitPassword(event.target.value)}
                  type="password"
                  value={cloudInitPassword}
                />
                <AppTextField
                  fullWidth
                  helperText={
                    cloudInitAuthProvided
                      ? undefined
                      : "Password or SSH key required"
                  }
                  id="vm-create-cloud-ssh-key"
                  label="SSH public key"
                  multiline
                  onChange={(event) => setCloudInitSSHKey(event.target.value)}
                  rows={3}
                  style={wideGridItemStyle}
                  value={cloudInitSSHKey}
                />
              </>
            ) : null}
          </div>
          <label style={checkboxLineStyle(theme)}>
            <AppCheckbox
              checked={start}
              onChange={(_, checked) => {
                markCustom();
                setStart(checked);
              }}
            />
            <span>Start after creation</span>
          </label>
          <div aria-label="Managed VM paths" style={managedPathsStyle(theme)}>
            {usesISO ? (
              <span style={managedPathChipStyle(theme)}>
                ISO folder{" "}
                <code
                  style={{
                    ...wrappingCodeStyle,
                    color: theme.palette.text.primary,
                  }}
                >
                  {managedISOPath}
                </code>
              </span>
            ) : (
              <span style={managedPathChipStyle(theme)}>
                Image folder{" "}
                <code
                  style={{
                    ...wrappingCodeStyle,
                    color: theme.palette.text.primary,
                  }}
                >
                  {managedCloudPath}
                </code>
              </span>
            )}
          </div>
          {createProgress ? (
            <div
              aria-live="polite"
              style={{
                ...createProgressStyle(theme),
                ...(createProgress.phase === "error" && {
                  borderColor: theme.palette.error.main,
                }),
              }}
            >
              <div style={createProgressHeaderStyle(theme)}>
                <span style={{ minWidth: 0, overflowWrap: "anywhere" }}>
                  {createProgress.message || "Starting VM create job"}
                </span>
                {createProgress.percent !== undefined ? (
                  <strong>{createProgress.percent}%</strong>
                ) : null}
              </div>
              {isCreating ? (
                <AppLinearProgress
                  value={createProgress.percent ?? 0}
                  variant={
                    createProgress.percent !== undefined
                      ? "determinate"
                      : "indeterminate"
                  }
                />
              ) : null}
              {createProgress.path ? (
                <code
                  style={{
                    ...wrappingCodeStyle,
                    color: theme.palette.text.secondary,
                  }}
                >
                  {createProgress.path}
                </code>
              ) : null}
            </div>
          ) : null}
          {preflight.data && (
            <>
              <PreflightSummary preflight={preflight.data} />
              {(preflight.data.warnings ?? []).length > 0 && (
                <AppAlert severity="warning">
                  <AppAlertTitle>Preflight Warnings</AppAlertTitle>
                  <ul style={messageListStyle}>
                    {(preflight.data.warnings ?? []).map((warning) => (
                      <li key={warning}>{warning}</li>
                    ))}
                  </ul>
                </AppAlert>
              )}
              {(preflight.data.errors ?? []).length > 0 && (
                <AppAlert severity="error">
                  <AppAlertTitle>Preflight Errors</AppAlertTitle>
                  <ul style={messageListStyle}>
                    {(preflight.data.errors ?? []).map((error) => (
                      <li key={error}>{error}</li>
                    ))}
                  </ul>
                </AppAlert>
              )}
            </>
          )}
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={isBusy} onClick={onClose} variant="text">
            Cancel
          </AppButton>
          <AppButton
            disabled={!canSubmit}
            startIcon={
              isBusy ? (
                <AppCircularProgress color="inherit" size={16} />
              ) : (
                <Icon height={18} icon="mdi:plus" width={18} />
              )
            }
            type="submit"
            variant="contained"
          >
            Create
          </AppButton>
        </AppDialogActions>
      </form>
    </GeneralDialog>
  );
}
