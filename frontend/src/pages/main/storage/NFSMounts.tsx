import { useQueryClient } from "@tanstack/react-query";
import React, {
  useCallback,
  useEffect,
  useEffectEvent,
  useRef,
  useState,
} from "react";

import { CACHE_TTL_MS, jobSnapshotResult, linuxio, type NFSMount } from "@/api";
import NFSMountCard from "@/components/cards/NFSMountCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import PageLoader from "@/components/loaders/PageLoader";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppGrid from "@/components/ui/AppGrid";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useScopedToast } from "@/hooks/useScopedToast";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";
interface NFSMountsProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}
interface MountNFSDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}
interface RemoveDialogProps {
  mount: NFSMount | null;
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}
interface EditNFSDialogProps {
  mount: NFSMount | null;
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}
interface EditNFSFormProps {
  mount: NFSMount;
  onClose: () => void;
  onSuccess: () => void;
}

function getMountStatusLabel(mount: NFSMount): string {
  return mount.mounted ? "Mounted" : "Configured";
}

function getPersistenceLabel(mount: NFSMount): string {
  return mount.inFstab ? "Persistent" : "Temporary";
}

function buildMountOptionsFromEntry(mount: NFSMount): string {
  return (mount.options ?? []).join(",");
}

// NFS version choices surfaced as a dropdown (emitted as `vers=<n>`). An empty
// value lets the client/server negotiate the version (mount default).
const NFS_VERSION_OPTIONS: { value: string; label: string }[] = [
  { value: "", label: "Default (auto)" },
  { value: "3", label: "NFSv3" },
  { value: "4", label: "NFSv4" },
  { value: "4.0", label: "NFSv4.0" },
  { value: "4.1", label: "NFSv4.1" },
  { value: "4.2", label: "NFSv4.2" },
];

// Transport protocol (emitted as `proto=<netid>`). Empty = mount default.
const NFS_PROTO_OPTIONS: { value: string; label: string }[] = [
  { value: "", label: "Default (auto)" },
  { value: "tcp", label: "TCP" },
  { value: "udp", label: "UDP" },
];

// Local locking mechanism (emitted as `local_lock=<mechanism>`). Empty maps to
// the mount default (`none`); required as `all` when re-exporting via Samba.
const NFS_LOCALLOCK_OPTIONS: { value: string; label: string }[] = [
  { value: "", label: "Default (none)" },
  { value: "all", label: "all" },
  { value: "flock", label: "flock" },
  { value: "posix", label: "posix" },
];

// Boolean mount options surfaced as toggle chips. Descriptions are condensed
// from nfs(5)/mount(8) and shown as tooltips. Each value is a single token that
// is either present or absent in the options list.
const NFS_TOGGLE_OPTIONS: {
  value: string;
  label: string;
  description: string;
}[] = [
  {
    value: "hard",
    label: "hard",
    description:
      "Retry NFS requests indefinitely until the server responds (default). Safer for data integrity but can hang if the server is unreachable.",
  },
  {
    value: "soft",
    label: "soft",
    description:
      "Return an error after retrans retries instead of hanging when the server is unreachable. Faster failure but risks data corruption on writes.",
  },
  {
    value: "bg",
    label: "bg",
    description:
      "If the first mount attempt fails, keep retrying in the background instead of blocking (useful at boot for unavailable servers).",
  },
  {
    value: "nolock",
    label: "nolock",
    description:
      "Disable NLM file locking. Use for servers that don't support locks or for purely local access.",
  },
  {
    value: "noatime",
    label: "noatime",
    description:
      "Don't update file access times, reducing writes to the server.",
  },
  {
    value: "nodiratime",
    label: "nodiratime",
    description: "Don't update directory access times.",
  },
  {
    value: "noac",
    label: "noac",
    description:
      "Disable attribute caching for stronger cache coherence between clients, at a significant performance cost.",
  },
  {
    value: "nosuid",
    label: "nosuid",
    description:
      "Ignore set-user-ID and set-group-ID bits on files from this mount.",
  },
  {
    value: "nodev",
    label: "nodev",
    description:
      "Don't interpret character or block special devices on this mount.",
  },
  {
    value: "noexec",
    label: "noexec",
    description: "Don't allow direct execution of binaries on this mount.",
  },
];

const NFS_TOGGLE_VALUES = NFS_TOGGLE_OPTIONS.map((o) => o.value);

// Toggles that cannot be active simultaneously; selecting one clears its peers.
const NFS_MUTUALLY_EXCLUSIVE_TOGGLES: string[][] = [["hard", "soft"]];

// Tokens already represented by a dedicated control, plus pure system defaults
// we don't want to echo back into the free-text "custom" field.
const NFS_MANAGED_OPTIONS = new Set<string>([
  "ro",
  "rw", // read-only switch
  "_netdev", // mount-at-boot switch
  ...NFS_TOGGLE_VALUES, // toggle chips
  // System defaults hidden to keep the custom field clean.
  "defaults",
  "nofail",
  "auto",
  "noauto",
  "fg",
  "ac",
  "atime",
  "diratime",
  "relatime",
  "strictatime",
  "lazytime",
  "sync",
  "async",
  "exec",
  "suid",
  "dev",
  "intr",
  "nointr",
]);

function isManagedNFSOption(token: string): boolean {
  if (NFS_MANAGED_OPTIONS.has(token)) return true;
  const key = token.split("=")[0];
  return (
    key === "vers" ||
    key === "nfsvers" ||
    key === "proto" ||
    key === "local_lock"
  );
}

// Kernel-generated / reported-only options the client adds automatically. They
// must never be surfaced for editing or re-submitted on remount (e.g. a stale
// addr/clientaddr can break an NFSv4 remount after a network change).
const NFS_INTERNAL_OPTION_KEYS = new Set<string>([
  "addr",
  "clientaddr",
  "mountaddr",
  "mountvers",
  "mountproto",
  "mounthost",
  "mountport",
  "namlen",
]);

function isInternalNFSOption(token: string): boolean {
  return NFS_INTERNAL_OPTION_KEYS.has(token.split("=")[0]);
}

// Options not covered by a dedicated control, surfaced for free-text editing.
// Kernel-internal options are dropped so they neither clutter the field nor get
// re-submitted to mount(8).
function getCustomOptionsFromEntry(mount: NFSMount): string {
  return (mount.options ?? [])
    .filter((o) => !isManagedNFSOption(o) && !isInternalNFSOption(o))
    .join(",");
}

// Reads the value of a `key=value` option (first match wins).
function getNFSOptionValue(options: string[], keys: string[]): string {
  for (const opt of options) {
    const idx = opt.indexOf("=");
    if (idx === -1) continue;
    if (keys.includes(opt.slice(0, idx))) return opt.slice(idx + 1);
  }
  return "";
}

const MountEntryActions: React.FC<{
  mount: NFSMount;
  mountingMountpoint: string | null;
  onEdit: (mount: NFSMount) => void;
  onMount: (mount: NFSMount) => void;
  onUnmount: (mount: NFSMount) => void;
  onRemove: (mount: NFSMount) => void;
  nfsClientAvailable: boolean;
  nfsReason: string;
  stopPropagation?: boolean;
}> = ({
  mount,
  mountingMountpoint,
  onEdit,
  onMount,
  onUnmount,
  onRemove,
  nfsClientAvailable,
  nfsReason,
  stopPropagation = false,
}) => {
  const wrapClick =
    (handler: (mount: NFSMount) => void) =>
    (event: React.MouseEvent<HTMLButtonElement>) => {
      if (stopPropagation) {
        event.stopPropagation();
      }
      handler(mount);
    };

  const isMounting = mountingMountpoint === mount.mountpoint;
  const mountActionColor = mount.mounted
    ? "var(--app-palette-success-main)"
    : "var(--app-palette-text-secondary)";
  const mountActionDisabled = !mount.mounted && !nfsClientAvailable;
  const mountActionLabel = mount.mounted ? "Unmount entry" : "Mount entry";
  const mountActionTitle = mountActionDisabled
    ? nfsReason
    : isMounting
      ? "Mounting..."
      : mountActionLabel;

  return (
    <div
      style={{
        display: "flex",
        gap: 2,
        alignItems: "center",
        justifyContent: "flex-end",
        flexShrink: 0,
      }}
    >
      <AppActionIconButton
        ariaLabel="Edit entry"
        color="var(--app-palette-primary-main)"
        icon="mdi:pencil-outline"
        iconSize={18}
        label="Edit entry"
        onClick={wrapClick(onEdit)}
      />
      <AppActionIconButton
        ariaLabel={mountActionLabel}
        color={mountActionColor}
        disabled={isMounting || mountActionDisabled}
        icon={mount.mounted ? "mdi:link-variant" : "mdi:link-variant-off"}
        iconSize={18}
        label={mountActionTitle}
        onClick={wrapClick(mount.mounted ? onUnmount : onMount)}
      />
      <AppActionIconButton
        ariaLabel="Remove entry"
        color="var(--app-palette-error-main)"
        icon="mdi:trash-can-outline"
        iconSize={18}
        label="Remove entry"
        onClick={wrapClick(onRemove)}
      />
    </div>
  );
};

const MountNFSDialog: React.FC<MountNFSDialogProps> = ({
  open,
  onClose,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  const [server, setServer] = useState("");
  const [exportPath, setExportPath] = useState("");
  const [mountpoint, setMountpoint] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [mountAtBoot, setMountAtBoot] = useState(false);
  const [customOptions, setCustomOptions] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const [exports, setExports] = useState<string[]>([]);
  const [loadingExports, setLoadingExports] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const { mutate: mountNFS, isPending: isMounting } =
    linuxio.storage.mount_nfs.useMutation({
      onSuccess: (result) => {
        const mountResult = jobSnapshotResult(result);
        if (mountResult.warning) {
          toast.warning(mountResult.warning);
        } else {
          toast.success(`NFS share mounted at ${mountpoint}`);
        }
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_nfs_mounts.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to mount NFS share"),
        );
      },
    });
  const fetchExports = useEffectEvent(async (serverAddress: string) => {
    setLoadingExports(true);
    try {
      const result = await queryClient.fetchQuery(
        linuxio.storage.list_nfs_exports.queryOptions(serverAddress, {
          staleTime: CACHE_TTL_MS.THIRTY_SECONDS,
        }),
      );
      setExports(result || []);
    } catch {
      setExports([]);
    } finally {
      setLoadingExports(false);
    }
  });

  // Fetch exports when server changes (debounced)
  useEffect(() => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }
    debounceRef.current = setTimeout(
      () => {
        if (!server || server.length < 3) {
          setExports([]);
        } else {
          fetchExports(server);
        }
      },
      !server || server.length < 3 ? 0 : 500,
    );
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, [server]);
  const buildOptionsString = () => {
    const opts: string[] = [];
    opts.push(readOnly ? "ro" : "rw");
    if (mountAtBoot) {
      opts.push("_netdev");
    }
    if (customOptions.trim()) {
      opts.push(
        ...customOptions
          .split(",")
          .map((o) => o.trim())
          .filter(Boolean),
      );
    }
    return opts.join(",");
  };
  const handleMount = () => {
    if (!server || !exportPath || !mountpoint) {
      setValidationError("Server, export path, and mountpoint are required");
      return;
    }
    setValidationError(null);
    mountNFS({
      server,
      exportPath,
      mountpoint,
      options: buildOptionsString(),
      persist: mountAtBoot ? "true" : "false",
    });
  };
  const handleClose = () => {
    setServer("");
    setExportPath("");
    setMountpoint("");
    setReadOnly(false);
    setMountAtBoot(false);
    setCustomOptions("");
    setExports([]);
    setValidationError(null);
    onClose();
  };
  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>Mount NFS Share</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 8,
            marginTop: 4,
          }}
        >
          <AppTextField
            fullWidth
            label="NFS Server"
            onChange={(e) => setServer(e.target.value)}
            placeholder="e.g., 192.168.1.100 or nas.local"
            size="small"
            value={server}
          />
          <AppAutocomplete
            endAdornment={
              loadingExports ? <AppCircularProgress size={20} /> : null
            }
            freeSolo
            fullWidth
            label="Path on Server"
            loading={loadingExports}
            onChange={setExportPath}
            onInputChange={setExportPath}
            options={exports}
            placeholder="e.g., /shared/data"
            size="small"
            value={exportPath}
          />
          <AppTextField
            fullWidth
            label="Local Mountpoint"
            onChange={(e) => setMountpoint(e.target.value)}
            placeholder="e.g., /mnt/nfs/data"
            size="small"
            value={mountpoint}
          />
          <AppTypography
            style={{
              marginTop: 4,
            }}
            variant="subtitle2"
          >
            Mount Options
          </AppTypography>
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={mountAtBoot}
                onChange={(e) => setMountAtBoot(e.target.checked)}
              />
            }
            label="Mount at boot (add to /etc/fstab)"
          />
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={readOnly}
                onChange={(e) => setReadOnly(e.target.checked)}
              />
            }
            label="Mount read-only"
          />
          <AppTextField
            fullWidth
            helperText="Additional comma-separated mount options"
            label="Custom Mount Options"
            onChange={(e) => setCustomOptions(e.target.value)}
            placeholder="e.g., soft,timeo=100,retrans=2"
            size="small"
            value={customOptions}
          />
          {validationError && (
            <AppAlert severity="error">{validationError}</AppAlert>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isMounting} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isMounting}
          onClick={handleMount}
          variant="contained"
        >
          {isMounting ? "Mounting..." : "Mount"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const RemoveDialog: React.FC<RemoveDialogProps> = ({
  open,
  onClose,
  mount,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  const { mutate: removeEntry, isPending: isRemoving } =
    linuxio.storage.unmount_nfs.useMutation({
      onSuccess: (result) => {
        const removeResult = jobSnapshotResult(result);
        if (mount?.mounted) {
          toast.success(
            mount.inFstab
              ? `Removed ${mount.mountpoint}`
              : `Unmounted ${mount.mountpoint}`,
          );
        } else {
          toast.success(`Removed saved entry for ${mount?.mountpoint}`);
        }
        if (removeResult.warning) {
          toast.warning(removeResult.warning);
        }
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_nfs_mounts.queryKey(),
        });
        onSuccess();
        onClose();
      },
      onError: (error: Error) => {
        toast.error(getMutationErrorMessage(error, "Failed to remove entry"));
      },
    });

  const handleRemove = () => {
    if (!mount) return;
    removeEntry({ mountpoint: mount.mountpoint, removeFstab: "true" });
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <AppDialogTitle>Remove NFS Entry</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          {mount?.mounted
            ? mount.inFstab
              ? "This will unmount the NFS entry and remove it from /etc/fstab."
              : "This will unmount the NFS entry and remove its saved LinuxIO entry."
            : mount?.inFstab
              ? "This will remove the saved NFS entry from /etc/fstab."
              : "This will remove the saved LinuxIO entry for this NFS mount."}
        </AppDialogContentText>
        {mount && (
          <div
            style={{
              marginTop: 8,
              marginBottom: 8,
            }}
          >
            <AppTypography variant="body2">
              <strong>Source:</strong> {mount.source}
            </AppTypography>
            <AppTypography variant="body2">
              <strong>Mountpoint:</strong> {mount.mountpoint}
            </AppTypography>
          </div>
        )}
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isRemoving} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isRemoving}
          onClick={handleRemove}
          variant="contained"
        >
          {isRemoving ? "Removing..." : "Remove"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
// Thin wrapper that owns the dialog shell. The form body lives in a child that
// is keyed by mountpoint and only mounted while the dialog is open, so its lazy
// state initializers re-run with the current mount on every open (no effect-based
// prop syncing needed).
const EditNFSDialog: React.FC<EditNFSDialogProps> = ({
  open,
  onClose,
  mount,
  onSuccess,
}) => {
  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <AppDialogTitle>Edit NFS Mount Options</AppDialogTitle>
      {mount && (
        <EditNFSForm
          key={mount.mountpoint}
          mount={mount}
          onClose={onClose}
          onSuccess={onSuccess}
        />
      )}
    </GeneralDialog>
  );
};
const EditNFSForm: React.FC<EditNFSFormProps> = ({
  mount,
  onClose,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  // Server, export path, and mountpoint are the mount's fixed identity.
  const server = mount.server || "";
  const exportPath = mount.exportPath || "";
  const options = mount.options ?? [];

  // Local form state, initialized from the mount. This component remounts on
  // every open (and per mount), so these initializers always reflect the current
  // mount.
  const [readOnly, setReadOnly] = useState(() => options.includes("ro"));
  const [mountAtBoot, setMountAtBoot] = useState(() => mount.inFstab ?? false);
  const [nfsVersion, setNfsVersion] = useState(() =>
    getNFSOptionValue(options, ["vers", "nfsvers"]),
  );
  const [protocol, setProtocol] = useState(() =>
    getNFSOptionValue(options, ["proto"]),
  );
  // The kernel always reports local_lock; treat the "none" default as unset.
  const [localLock, setLocalLock] = useState(() => {
    const lock = getNFSOptionValue(options, ["local_lock"]);
    return lock === "none" ? "" : lock;
  });
  const [selectedToggles, setSelectedToggles] = useState<string[]>(() =>
    NFS_TOGGLE_VALUES.filter((v) => options.includes(v)),
  );
  const [customOptions, setCustomOptions] = useState(() =>
    getCustomOptionsFromEntry(mount),
  );

  const toggleOption = (value: string) => {
    setSelectedToggles((prev) => {
      if (prev.includes(value)) {
        return prev.filter((v) => v !== value);
      }
      const group = NFS_MUTUALLY_EXCLUSIVE_TOGGLES.find((g) =>
        g.includes(value),
      );
      const base = group ? prev.filter((v) => !group.includes(v)) : prev;
      return [...base, value];
    });
  };
  const { mutate: remountNFS, isPending: isRemounting } =
    linuxio.storage.remount_nfs.useMutation({
      onSuccess: (result) => {
        const remountResult = jobSnapshotResult(result);
        if (remountResult.warning) {
          toast.warning(remountResult.warning);
        } else {
          toast.success(`NFS mount options updated`);
        }
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_nfs_mounts.queryKey(),
        });
        onSuccess();
        onClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to update mount options"),
        );
      },
    });
  const buildOptionsString = () => {
    const opts: string[] = [];
    opts.push(readOnly ? "ro" : "rw");
    if (mountAtBoot) {
      opts.push("_netdev");
    }
    if (nfsVersion) {
      opts.push(`vers=${nfsVersion}`);
    }
    if (protocol) {
      opts.push(`proto=${protocol}`);
    }
    if (localLock) {
      opts.push(`local_lock=${localLock}`);
    }
    // Emit toggles in catalog order for a stable, predictable options string.
    for (const o of NFS_TOGGLE_OPTIONS) {
      if (selectedToggles.includes(o.value)) {
        opts.push(o.value);
      }
    }
    if (customOptions.trim()) {
      opts.push(
        ...customOptions
          .split(",")
          .map((o) => o.trim())
          .filter(Boolean),
      );
    }
    // De-duplicate while preserving order (custom field may repeat a managed token).
    return Array.from(new Set(opts)).join(",");
  };
  const handleSave = () => {
    remountNFS({
      mountpoint: mount.mountpoint,
      options: buildOptionsString(),
      updateFstab: mountAtBoot ? "true" : "false",
    });
  };
  return (
    <>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 8,
            marginTop: 4,
          }}
        >
          <AppTextField
            disabled
            fullWidth
            label="Server Address"
            size="small"
            value={server}
          />
          <AppTextField
            disabled
            fullWidth
            label="Path on Server"
            size="small"
            value={exportPath}
          />
          <AppTextField
            disabled
            fullWidth
            label="Local Mountpoint"
            size="small"
            value={mount.mountpoint}
          />
          <AppTypography color="text.secondary" variant="caption">
            Server, path, and mountpoint are fixed for an existing mount. To
            change them, remove this entry and add it again.
          </AppTypography>
          <AppTypography
            style={{
              marginTop: 4,
            }}
            variant="subtitle2"
          >
            Mount Options
          </AppTypography>
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={mountAtBoot}
                onChange={(e) => setMountAtBoot(e.target.checked)}
              />
            }
            label="Mount at boot (add to /etc/fstab)"
          />
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={readOnly}
                onChange={(e) => setReadOnly(e.target.checked)}
              />
            }
            label="Mount read-only"
          />
          <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
            <div style={{ flex: "1 1 140px", minWidth: 0 }}>
              <AppSelect
                fullWidth
                label="NFS version"
                onChange={(e) => setNfsVersion(e.target.value)}
                size="small"
                value={nfsVersion}
              >
                {NFS_VERSION_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </AppSelect>
            </div>
            <div style={{ flex: "1 1 140px", minWidth: 0 }}>
              <AppSelect
                fullWidth
                label="Protocol"
                onChange={(e) => setProtocol(e.target.value)}
                size="small"
                value={protocol}
              >
                {NFS_PROTO_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </AppSelect>
            </div>
            <div style={{ flex: "1 1 140px", minWidth: 0 }}>
              <AppSelect
                fullWidth
                label="Local lock"
                onChange={(e) => setLocalLock(e.target.value)}
                size="small"
                value={localLock}
              >
                {NFS_LOCALLOCK_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </AppSelect>
            </div>
          </div>
          <AppTypography color="text.secondary" variant="caption">
            Common options
          </AppTypography>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
            {NFS_TOGGLE_OPTIONS.map((o) => {
              const isSelected = selectedToggles.includes(o.value);
              return (
                <AppTooltip key={o.value} title={o.description}>
                  <Chip
                    color={isSelected ? "primary" : "default"}
                    label={o.label}
                    onClick={() => toggleOption(o.value)}
                    size="small"
                    variant={isSelected ? "soft" : "outlined"}
                  />
                </AppTooltip>
              );
            })}
          </div>
          <AppTextField
            fullWidth
            helperText="Additional comma-separated mount options (e.g. rsize=, wsize=, timeo=, retrans=, sec=, port=)"
            label="Custom Mount Options"
            onChange={(e) => setCustomOptions(e.target.value)}
            placeholder="e.g., timeo=100,retrans=2,rsize=1048576"
            size="small"
            value={customOptions}
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isRemounting} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isRemounting}
          onClick={handleSave}
          variant="contained"
        >
          {isRemounting ? "Saving..." : "Save"}
        </AppButton>
      </AppDialogActions>
    </>
  );
};
const NFSMounts: React.FC<NFSMountsProps> = ({
  onMountCreateHandler,
  viewMode = "table",
}) => {
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  const { reason: nfsReason, status: nfsStatus } =
    useCapability("nfsClientAvailable");
  const nfsUnavailable = nfsStatus === "unavailable";
  const [search] = useState("");
  const [mountDialogOpen, setMountDialogOpen] = useState(false);
  const [removeDialogOpen, setRemoveDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [selectedMount, setSelectedMount] = useState<NFSMount | null>(null);
  const [mountingMountpoint, setMountingMountpoint] = useState<string | null>(
    null,
  );
  const {
    data: mounts = [],
    isPending: loading,
    refetch,
  } = linuxio.storage.list_nfs_mounts.useQuery({
    refetchInterval: 10000,
  });
  const { mutate: mountExistingEntry } = linuxio.storage.mount_nfs.useMutation({
    onSuccess: (result) => {
      const mountResult = jobSnapshotResult(result);
      if (mountResult.warning) {
        toast.warning(mountResult.warning);
      } else {
        toast.success("NFS entry mounted");
      }
      setMountingMountpoint(null);
      refetch();
    },
    onError: (error: Error) => {
      setMountingMountpoint(null);
      toast.error(getMutationErrorMessage(error, "Failed to mount NFS entry"));
    },
  });
  const { mutate: unmountEntry } = linuxio.storage.unmount_nfs.useMutation({
    onSuccess: (result, variables) => {
      const unmountResult = jobSnapshotResult(result);
      if (unmountResult.warning) {
        toast.warning(unmountResult.warning);
      } else {
        toast.success(`Unmounted ${variables.mountpoint}`);
      }
      refetch();
    },
    onError: (error: Error) => {
      toast.error(getMutationErrorMessage(error, "Failed to unmount"));
    },
  });
  const handleMountNFS = useCallback(() => {
    if (nfsUnavailable) {
      toast.error(nfsReason);
      return;
    }
    setMountDialogOpen(true);
  }, [nfsUnavailable, nfsReason, toast]);
  useRegisterCreateHandler(onMountCreateHandler, handleMountNFS);
  const handleUnmount = (mount: NFSMount) => {
    unmountEntry({ mountpoint: mount.mountpoint, removeFstab: "false" });
  };
  const handleEdit = (mount: NFSMount) => {
    setSelectedMount(mount);
    setEditDialogOpen(true);
  };
  const handleRemove = (mount: NFSMount) => {
    setSelectedMount(mount);
    setRemoveDialogOpen(true);
  };
  const handleMountExisting = (mount: NFSMount) => {
    if (nfsUnavailable) {
      toast.error(nfsReason);
      return;
    }
    if (!mount.server || !mount.exportPath) {
      toast.error("This NFS entry is missing its server or export path");
      return;
    }
    setMountingMountpoint(mount.mountpoint);
    mountExistingEntry({
      server: mount.server,
      exportPath: mount.exportPath,
      mountpoint: mount.mountpoint,
      options: buildMountOptionsFromEntry(mount),
      persist: mount.inFstab ? "true" : "false",
    });
  };
  if (loading) {
    return <PageLoader />;
  }
  const mountsList = Array.isArray(mounts) ? mounts : [];
  const filtered = mountsList.filter(
    (m) =>
      m.source.toLowerCase().includes(search.toLowerCase()) ||
      m.mountpoint.toLowerCase().includes(search.toLowerCase()) ||
      getMountStatusLabel(m).toLowerCase().includes(search.toLowerCase()),
  );
  const columns: AppDataTableColumnDef<(typeof filtered)[number]>[] = [
    {
      accessorKey: "source",
      header: "NFS Share",
      cell: ({ row }) => (
        <AppTypography
          style={{
            fontFamily: "monospace",
          }}
          variant="body2"
        >
          {row.original.source}
        </AppTypography>
      ),
      meta: { align: "left" },
    },
    {
      accessorKey: "mountpoint",
      header: "Mount Point",
      cell: ({ row }) => (
        <AppTypography
          style={{
            fontFamily: "monospace",
          }}
          variant="body2"
        >
          {row.original.mountpoint}
        </AppTypography>
      ),
      meta: { align: "left" },
    },
    {
      id: "status",
      header: "Status",
      accessorFn: (mount) =>
        `${getMountStatusLabel(mount)} ${getPersistenceLabel(mount)}`,
      cell: ({ row }) => {
        const mount = row.original;
        return (
          <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
            <Chip
              label={getMountStatusLabel(mount)}
              size="small"
              variant="soft"
            />
            <Chip
              label={getPersistenceLabel(mount)}
              size="small"
              variant="soft"
            />
          </div>
        );
      },
      meta: {
        align: "left",
        width: "160px",
      },
    },
    {
      accessorKey: "usedPct",
      header: "Usage",
      cell: ({ row }) => {
        const mount = row.original;
        return mount.mounted ? (
          <div
            style={{
              width: "100%",
            }}
          >
            <AppLinearProgress
              color={
                mount.usedPct > 90
                  ? "error"
                  : mount.usedPct > 70
                    ? "warning"
                    : "primary"
              }
              style={{
                height: 6,
                borderRadius: 3,
                marginBottom: 2,
              }}
              value={mount.usedPct}
              variant="determinate"
            />
            <AppTypography color="text.secondary" variant="caption">
              {formatFileSize(mount.used)} / {formatFileSize(mount.size)}
            </AppTypography>
          </div>
        ) : (
          <AppTypography color="text.secondary" variant="caption">
            Not mounted
          </AppTypography>
        );
      },
      meta: {
        align: "left",
        hideBelow: "sm",
        width: "200px",
      },
    },
    {
      id: "actions",
      header: "",
      enableSorting: false,
      cell: ({ row }) => (
        <MountEntryActions
          mount={row.original}
          mountingMountpoint={mountingMountpoint}
          nfsClientAvailable={!nfsUnavailable}
          nfsReason={nfsReason}
          onEdit={handleEdit}
          onMount={handleMountExisting}
          onRemove={handleRemove}
          onUnmount={handleUnmount}
          stopPropagation
        />
      ),
      meta: {
        align: "right",
        width: "160px",
      },
    },
  ];
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      {nfsUnavailable ? (
        <AppAlert severity="warning">{nfsReason}</AppAlert>
      ) : null}

      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <AppGrid container spacing={2}>
            {filtered.map((mount) => (
              <AppGrid
                key={mount.mountpoint}
                size={{
                  xs: 12,
                  sm: 6,
                  md: 4,
                  lg: 3,
                }}
              >
                <NFSMountCard
                  actions={
                    <MountEntryActions
                      mount={mount}
                      mountingMountpoint={mountingMountpoint}
                      nfsClientAvailable={!nfsUnavailable}
                      nfsReason={nfsReason}
                      onEdit={handleEdit}
                      onMount={handleMountExisting}
                      onRemove={handleRemove}
                      onUnmount={handleUnmount}
                    />
                  }
                  mount={mount}
                  persistenceLabel={getPersistenceLabel(mount)}
                  statusLabel={getMountStatusLabel(mount)}
                />
              </AppGrid>
            ))}
          </AppGrid>
        ) : (
          <div
            style={{
              textAlign: "center",
              paddingBlock: 16,
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              No NFS entries found. Click Mount NFS to add one.
            </AppTypography>
          </div>
        )
      ) : (
        <AppDataTable
          ariaLabel="NFS mounts"
          columns={columns}
          data={filtered}
          emptyMessage="No NFS entries found. Click 'Mount NFS' to add one."
          fillAvailable
          getRowId={(mount) => mount.mountpoint}
          renderExpandedContent={({ original: mount }) => (
            <div className="expand-panel">
              <AppTypography gutterBottom variant="subtitle2">
                <strong>Status:</strong> {getMountStatusLabel(mount)} /{" "}
                {getPersistenceLabel(mount)}
              </AppTypography>
              <div>
                <AppTypography gutterBottom variant="subtitle2">
                  <strong>Options:</strong>
                </AppTypography>
                <div className="expand-panel__chips">
                  {mount.options && mount.options.length > 0 ? (
                    mount.options.map((opt, i) => (
                      <Chip key={i} label={opt} size="small" variant="soft" />
                    ))
                  ) : (
                    <AppTypography color="text.secondary" variant="body2">
                      (no options)
                    </AppTypography>
                  )}
                </div>
              </div>
              <AppTypography gutterBottom variant="subtitle2">
                <strong>Filesystem Type:</strong> {mount.fsType}
              </AppTypography>
              {mount.mounted ? (
                <AppTypography gutterBottom variant="subtitle2">
                  <strong>Storage:</strong> {formatFileSize(mount.used)} used of{" "}
                  {formatFileSize(mount.size)} ({mount.usedPct.toFixed(1)}%
                  used, {formatFileSize(mount.free)} free)
                </AppTypography>
              ) : (
                <AppTypography gutterBottom variant="subtitle2">
                  <strong>Storage:</strong> Not currently mounted
                </AppTypography>
              )}
            </div>
          )}
        />
      )}

      <MountNFSDialog
        onClose={() => setMountDialogOpen(false)}
        onSuccess={() => refetch()}
        open={mountDialogOpen}
      />

      <EditNFSDialog
        mount={selectedMount}
        onClose={() => setEditDialogOpen(false)}
        onSuccess={() => refetch()}
        open={editDialogOpen}
      />

      <RemoveDialog
        mount={selectedMount}
        onClose={() => setRemoveDialogOpen(false)}
        onSuccess={() => refetch()}
        open={removeDialogOpen}
      />
    </div>
  );
};
export default NFSMounts;
