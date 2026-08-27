import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import {
  useCallback,
  useState,
  type CSSProperties,
  type SyntheticEvent,
} from "react";

import { call, linuxio, useCallMutation } from "@/api";
import type { VMCreateProgress, VMCreateRequest } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
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
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import PathPickerField from "@/components/ui/PathPickerField";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppMediaQuery } from "@/theme";
import { down } from "@/theme/breakpoints";
import { getMutationErrorMessage } from "@/utils/mutations";
import { ensureTrailingSlash } from "@/utils/path";

import PreflightSummary from "./PreflightSummary";
import type {
  ReadyImagePreset,
  VMCreateMode,
  VMDialogImagePresetID,
  VMDialogSourceType,
  VMPresetID,
} from "./vmShared";
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

const createModeStyle = (isMobile: boolean): CSSProperties =>
  isMobile
    ? {
        display: "grid",
        gap: "var(--app-space-8)",
        gridTemplateColumns: "repeat(2, minmax(0, 1fr))",
        marginBottom: "var(--app-space-12)",
      }
    : {
        display: "inline-flex",
        gap: "var(--app-space-8)",
        marginBottom: "var(--app-space-12)",
      };

const presetGroupStyle = (isMobile: boolean): CSSProperties => ({
  display: "grid",
  gap: "var(--app-space-8)",
  gridTemplateColumns: isMobile
    ? "1fr"
    : "repeat(auto-fit, minmax(180px, 1fr))",
  marginBottom: "var(--app-space-16)",
});

const presetButtonStyle: CSSProperties = {
  alignItems: "flex-start",
  flexDirection: "column",
  gap: 2,
  minHeight: 56,
  minWidth: 0,
  padding: "8px 10px",
};

const formGridStyle = (isMobile: boolean): CSSProperties => ({
  display: "grid",
  gap: "var(--app-space-16)",
  gridTemplateColumns: isMobile ? "1fr" : "repeat(2, minmax(0, 1fr))",
});

const wideGridItemStyle: CSSProperties = {
  gridColumn: "1 / -1",
};

const checkboxLineStyle: CSSProperties = {
  alignItems: "center",
  display: "inline-flex",
  gap: "var(--app-space-8)",
  margin: "var(--app-space-16) 0",
};

const managedPathsStyle: CSSProperties = {
  display: "flex",
  flexWrap: "wrap",
  gap: "var(--app-space-8)",
  margin: "var(--app-space-2) 0 var(--app-space-12)",
};

const managedPathChipStyle: CSSProperties = {
  alignItems: "center",
  border: "1px solid var(--app-palette-divider)",
  borderRadius: 6,
  color: "var(--app-palette-text-secondary)",
  display: "inline-flex",
  gap: "var(--app-space-8)",
  minWidth: 0,
  padding: "var(--app-space-6) var(--app-space-8)",
};

const createProgressStyle: CSSProperties = {
  border: "1px solid var(--app-palette-divider)",
  borderRadius: 6,
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-8)",
  marginBottom: "var(--app-space-12)",
  padding: "var(--app-space-8)",
};

const createProgressHeaderStyle: CSSProperties = {
  alignItems: "center",
  display: "flex",
  gap: "var(--app-space-12)",
  justifyContent: "space-between",
  minWidth: 0,
};

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
  const isMobile = useAppMediaQuery(down("sm"));
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
  const [network, setNetwork] = useState("default");
  const networksQuery = useQuery({
    ...linuxio.virt.networks,
    enabled: open,
    refetchInterval: open ? 30000 : false,
  });
  const networks = networksQuery.data ?? [];
  const hostBridges = networks.filter(
    (candidate) => candidate.type === "bridge",
  );
  const activeHostBridges = hostBridges.filter((candidate) => candidate.active);
  const usesISO = sourceType === "iso";
  const usesCloudInit = Boolean(
    imagePresetId && CLOUD_INIT_IMAGE_PRESETS.has(imagePresetId),
  );
  // Available networks drive selection readiness, and create revalidates the
  // choice server-side. Keep the full host/source preflight independent of the
  // dropdown so changing networks does not repeat every host probe.
  const preflight = useQuery({
    ...linuxio.virt.preflight({
      imagePresetId,
      isoPath: usesISO ? isoPath || undefined : undefined,
      sourceType,
    }),
    enabled: open,
    refetchInterval: open ? 5000 : false,
  });
  const { refetch: refetchPreflight } = preflight;
  const createISOFolderMutation = useCallMutation(
    linuxio.filebrowser.resource_post,
    {
      invalidates: (_result, variables) => [
        linuxio.filebrowser.resource_get({
          path: ensureTrailingSlash(parentDirectory(variables.path) || "/"),
        }).queryKey,
      ],
    },
  );
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
    setNetwork(
      preset.bridgedPreferred && activeHostBridges.length === 1
        ? activeHostBridges[0].name
        : "default",
    );
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
    setNetwork("default");
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
  const selectedNetwork = networks.find(
    (candidate) => candidate.name === network,
  );
  const networkAvailable =
    network === "default"
      ? selectedNetwork?.type === "libvirt"
      : selectedNetwork?.type === "bridge" && selectedNetwork.active;
  // The base preflight still describes the optional default NAT network.
  // Those messages do not apply when the VM will attach to a host bridge.
  const isSelectedNetworkMessage = (message: string) =>
    network === "default" ||
    !message.toLowerCase().includes("default nat network");
  const preflightWarnings = (preflight.data?.warnings ?? []).filter(
    isSelectedNetworkMessage,
  );
  const preflightErrors = (preflight.data?.errors ?? []).filter(
    isSelectedNetworkMessage,
  );
  const hasBlockingPreflightErrors = preflightErrors.length > 0;
  const isBusy = isCreating || createISOFolderMutation.isPending;
  const canSubmit =
    fieldsValid &&
    networkAvailable &&
    !isBusy &&
    !networksQuery.isPending &&
    !networksQuery.isError &&
    !preflight.isLoading &&
    !preflight.isLoadingError &&
    !hasBlockingPreflightErrors;

  const ensureISOFolderExists = useCallback(async () => {
    if (!usesISO) return;
    const folder = folderFromISOPathText(isoPath);
    if (!folder || folder === "/") return;

    try {
      const stat = await call(linuxio.filebrowser.resource_stat.route, {
        path: folder,
      });
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
      void refetchPreflight();
    } catch (error) {
      toast.error(
        getMutationErrorMessage(error, "Failed to create ISO folder"),
      );
    }
  }, [createISOFolderMutation, isoPath, refetchPreflight, toast, usesISO]);

  const handleSubmit = (event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) return;
    const request: VMCreateRequest = {
      diskGB: parsedDiskGB,
      memoryMB: parsedMemoryMB,
      name: name.trim(),
      network,
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
            style={createModeStyle(isMobile)}
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
              style={presetGroupStyle(isMobile)}
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
                  <AppTypography
                    color="inherit"
                    component="small"
                    fontWeight={400}
                    style={{ lineHeight: 1.35, opacity: 0.74 }}
                    variant="caption"
                  >
                    {preset.vcpus} CPU /{" "}
                    {Number.parseInt(preset.memoryMB, 10) / 1024} GB /{" "}
                    {preset.diskGB} GB
                  </AppTypography>
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
                  <AppTypography
                    color="inherit"
                    component="small"
                    fontWeight={400}
                    style={{ lineHeight: 1.35, opacity: 0.74 }}
                    variant="caption"
                  >
                    Manual sizing
                  </AppTypography>
                </AppButton>
              )}
            </div>
          ) : null}
          <div style={formGridStyle(isMobile)}>
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
            <AppSelect
              disabled={
                isBusy || networksQuery.isPending || networksQuery.isError
              }
              fullWidth
              label="Network"
              onChange={(event) => setNetwork(event.target.value)}
              value={network}
            >
              <option value="default">NAT (default)</option>
              {hostBridges.map((bridge) => (
                <option
                  disabled={!bridge.active}
                  key={bridge.name}
                  value={bridge.name}
                >
                  {bridge.name}
                  {!bridge.active ? " (inactive)" : ""}
                </option>
              ))}
            </AppSelect>
            {networksQuery.isError ? (
              <AppAlert severity="error" style={wideGridItemStyle}>
                Unable to load the available VM networks.
              </AppAlert>
            ) : !networksQuery.isPending && !networkAvailable ? (
              <AppAlert severity="error" style={wideGridItemStyle}>
                The selected VM network is unavailable.
              </AppAlert>
            ) : null}
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
          <label style={checkboxLineStyle}>
            <AppCheckbox
              checked={start}
              onChange={(_, checked) => {
                markCustom();
                setStart(checked);
              }}
            />
            <span>Start after creation</span>
          </label>
          <div aria-label="Managed VM paths" style={managedPathsStyle}>
            {usesISO ? (
              <span style={managedPathChipStyle}>
                ISO folder{" "}
                <code
                  style={{
                    ...wrappingCodeStyle,
                    color: "var(--app-palette-text-primary)",
                  }}
                >
                  {managedISOPath}
                </code>
              </span>
            ) : (
              <span style={managedPathChipStyle}>
                Image folder{" "}
                <code
                  style={{
                    ...wrappingCodeStyle,
                    color: "var(--app-palette-text-primary)",
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
                ...createProgressStyle,
                ...(createProgress.phase === "error" && {
                  borderColor: "var(--app-palette-error-main)",
                }),
              }}
            >
              <div style={createProgressHeaderStyle}>
                <span style={{ minWidth: 0, overflowWrap: "anywhere" }}>
                  {createProgress.message || "Starting VM create task"}
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
                    color: "var(--app-palette-text-secondary)",
                  }}
                >
                  {createProgress.path}
                </code>
              ) : null}
            </div>
          ) : null}
          {preflight.isLoading ? (
            <div style={{ padding: "var(--app-space-8)" }}>
              <ComponentLoader />
            </div>
          ) : preflight.isLoadingError ? (
            <AppAlert severity="error">
              <AppAlertTitle>Preflight unavailable</AppAlertTitle>
              {preflight.error.message}
            </AppAlert>
          ) : preflight.data ? (
            <>
              <PreflightSummary
                preflight={preflight.data}
                showDefaultNetwork={network === "default"}
              />
              {preflightWarnings.length > 0 && (
                <AppAlert severity="warning">
                  <AppAlertTitle>Preflight Warnings</AppAlertTitle>
                  <ul style={messageListStyle}>
                    {preflightWarnings.map((warning) => (
                      <li key={warning}>{warning}</li>
                    ))}
                  </ul>
                </AppAlert>
              )}
              {preflightErrors.length > 0 && (
                <AppAlert severity="error">
                  <AppAlertTitle>Preflight Errors</AppAlertTitle>
                  <ul style={messageListStyle}>
                    {preflightErrors.map((error) => (
                      <li key={error}>{error}</li>
                    ))}
                  </ul>
                </AppAlert>
              )}
            </>
          ) : null}
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
