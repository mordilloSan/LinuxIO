import { useState } from "react";

import type {
  PartitionCreateRequest,
  PartitionFormatRequest,
  PartitionMountRequest,
  StorageBlockDevice,
  StorageRegion,
} from "@/api";
import type { CapabilityKey } from "@/api/capabilities";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppRouterLinkButton from "@/components/ui/AppRouterLinkButton";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { formatFileSize } from "@/utils/formatters";

const MiB = 1024 ** 2;
const UNITS = { MiB, GiB: 1024 * MiB, TiB: 1024 ** 2 * MiB } as const;
type Unit = keyof typeof UNITS;

/** Longest label each filesystem takes, in UTF-8 bytes. */
const LABEL_LIMITS: Record<string, number> = { ext4: 16, xfs: 12, btrfs: 255 };

/** Smallest partition each filesystem's mkfs takes, as btrfs.MinFormatSize. */
const MIN_SIZES: Record<string, number> = {
  ext4: MiB,
  xfs: 300 * MiB,
  btrfs: 109 * MiB,
};

const FS_CAPABILITY: Record<string, CapabilityKey> = {
  ext4: "e2fsprogsAvailable",
  xfs: "xfsprogsAvailable",
  btrfs: "btrfsAvailable",
};

const labelError = (fsType: string, label: string) => {
  const limit = LABEL_LIMITS[fsType];
  if (limit && new TextEncoder().encode(label).length > limit) {
    return `${fsType} labels take at most ${limit} bytes`;
  }
  if (label.split("").some((c) => c === "/" || c < " " || c === "\u007f")) {
    return "A label cannot contain / or control characters";
  }
  return null;
};

const validMountpoint = (path: string) =>
  path.startsWith("/") && path.length > 1 && !path.endsWith("/");

const stack = {
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-12)",
} as const;

const Progress = ({ step }: { step?: string | null }) => (
  <div role="status">
    <AppLinearProgress aria-label="Working" />
    <AppTypography color="text.secondary" variant="caption">
      {step ?? "Starting..."}
    </AppTypography>
  </div>
);

interface FormatFieldsProps {
  fsType: string;
  label: string;
  /** The partition's size: a filesystem it is too small for is disabled. */
  sizeBytes: number;
  /** Offers "No filesystem" as a choice, for a new partition. */
  optional?: boolean;
  onFsType: (fsType: string) => void;
  onLabel: (label: string) => void;
}

/** Whether the tools that make `fsType` are installed. */
const useFsAvailable = () => {
  const available = {
    ext4: useCapability(FS_CAPABILITY.ext4),
    xfs: useCapability(FS_CAPABILITY.xfs),
    btrfs: useCapability(FS_CAPABILITY.btrfs),
  };
  return (fsType: string) =>
    fsType in available &&
    available[fsType as keyof typeof available].isEnabled;
};

const FormatFields = ({
  fsType,
  label,
  sizeBytes,
  optional = false,
  onFsType,
  onLabel,
}: FormatFieldsProps) => {
  const available = useFsAvailable();
  const unavailable = Object.keys(LABEL_LIMITS).filter((fs) => !available(fs));
  const error = fsType ? labelError(fsType, label) : null;
  return (
    <>
      <AppSelect
        fullWidth
        label="Filesystem"
        onChange={(event) => onFsType(event.target.value)}
        value={fsType}
      >
        {optional && <option value="">No filesystem</option>}
        {Object.keys(LABEL_LIMITS).map((fs) => {
          const tooSmall = sizeBytes < MIN_SIZES[fs];
          return (
            <option disabled={!available(fs) || tooSmall} key={fs} value={fs}>
              {!available(fs)
                ? `${fs} (unavailable)`
                : tooSmall
                  ? `${fs} (needs ${MIN_SIZES[fs] / MiB} MiB)`
                  : fs}
            </option>
          );
        })}
      </AppSelect>
      {unavailable.length > 0 && (
        <AppTypography color="text.secondary" variant="caption">
          {`Not installed: ${unavailable.join(", ")}. `}
          <AppRouterLinkButton
            search={{ tab: "capabilities" }}
            size="small"
            to="/settings"
          >
            Install them in Settings
          </AppRouterLinkButton>
        </AppTypography>
      )}
      {fsType && (
        <AppTextField
          error={error !== null}
          fullWidth
          helperText={error ?? `Optional, up to ${LABEL_LIMITS[fsType]} bytes`}
          label="Label"
          onChange={(event) => onLabel(event.target.value)}
          value={label}
        />
      )}
    </>
  );
};

interface MountFieldsProps {
  mountpoint: string;
  persist: boolean;
  onMountpoint: (path: string) => void;
  onPersist: (persist: boolean) => void;
}

const MountFields = ({
  mountpoint,
  persist,
  onMountpoint,
  onPersist,
}: MountFieldsProps) => (
  <>
    <AppTextField
      error={!validMountpoint(mountpoint)}
      fullWidth
      label="Mount point"
      onChange={(event) => onMountpoint(event.target.value)}
      value={mountpoint}
    />
    <AppFormControlLabel
      control={
        <AppCheckbox
          checked={persist}
          onChange={(_, checked) => onPersist(checked)}
        />
      }
      label="Mount at boot"
    />
  </>
);

interface DialogProps {
  isPending: boolean;
  /** Why the last attempt failed, so the form can be corrected and resent. */
  error?: string | null;
  /** The step a running task is on. */
  step?: string | null;
  onClose: () => void;
}

/** The size the region holds, in the largest unit that divides it exactly. */
const wholeSize = (bytes: number): { size: string; unit: Unit } => {
  const unit = (["TiB", "GiB"] as const).find((u) => bytes % UNITS[u] === 0);
  return { size: String(bytes / UNITS[unit ?? "MiB"]), unit: unit ?? "MiB" };
};

export const CreatePartitionDialog = ({
  disk,
  region,
  isPending,
  error,
  step,
  onClose,
  onConfirm,
}: DialogProps & {
  disk: StorageBlockDevice;
  region: StorageRegion;
  onConfirm: (request: PartitionCreateRequest) => void;
}) => {
  const initial = wholeSize(region.sizeBytes);
  const [size, setSize] = useState(initial.size);
  const [unit, setUnit] = useState<Unit>(initial.unit);
  const [fsType, setFsType] = useState("");
  const [label, setLabel] = useState("");
  const [mounting, setMounting] = useState(false);
  // null follows the label until the mount point is edited.
  const [mountpoint, setMountpoint] = useState<string | null>(null);
  const [persist, setPersist] = useState(false);
  const available = useFsAvailable();

  const target = mountpoint ?? `/mnt/${label.trim()}`;
  // Whole MiB, as the partition table is aligned to them.
  const sizeBytes = Math.floor((Number(size) * UNITS[unit]) / MiB) * MiB;
  const mount = mounting && fsType !== "";
  const valid =
    sizeBytes >= MiB &&
    (fsType === "" ||
      (available(fsType) &&
        sizeBytes >= MIN_SIZES[fsType] &&
        labelError(fsType, label) === null)) &&
    (!mount || validMountpoint(target));

  // A size over the region becomes the largest that fits.
  const resize = (next: string, nextUnit: Unit) => {
    const limit = region.sizeBytes / UNITS[nextUnit];
    setUnit(nextUnit);
    setSize(
      Number(next) > limit
        ? String(
            Number.isInteger(limit) ? limit : Math.floor(limit * 100) / 100,
          )
        : next,
    );
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open>
      <AppDialogTitle>Create partition</AppDialogTitle>
      <AppDialogContent>
        <div style={stack}>
          <AppTypography color="text.secondary" variant="body2">
            {`${formatFileSize(region.sizeBytes)} free on ${disk.path}`}
          </AppTypography>
          <div style={{ display: "flex", gap: "var(--app-space-8)" }}>
            <AppTextField
              fullWidth
              label="Size"
              onChange={(event) => resize(event.target.value, unit)}
              value={size}
            />
            <AppSelect
              label="Unit"
              onChange={(event) => resize(size, event.target.value as Unit)}
              value={unit}
            >
              {Object.keys(UNITS).map((u) => (
                <option key={u} value={u}>
                  {u}
                </option>
              ))}
            </AppSelect>
          </div>
          <FormatFields
            fsType={fsType}
            label={label}
            onFsType={setFsType}
            onLabel={setLabel}
            optional
            sizeBytes={sizeBytes}
          />
          {fsType !== "" && (
            <AppFormControlLabel
              control={
                <AppSwitch
                  checked={mounting}
                  onChange={(_, checked) => setMounting(checked)}
                />
              }
              label="Mount after formatting"
            />
          )}
          {mount && (
            <MountFields
              mountpoint={target}
              onMountpoint={setMountpoint}
              onPersist={setPersist}
              persist={persist}
            />
          )}
          {isPending && <Progress step={step} />}
          {error && <AppAlert severity="error">{error}</AppAlert>}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isPending || !valid}
          onClick={() =>
            onConfirm({
              device: disk.path,
              startBytes: region.startBytes,
              sizeBytes,
              ...(fsType && { format: { fsType, label } }),
              ...(mount && { mount: { mountpoint: target, persist } }),
            })
          }
          variant="contained"
        >
          {isPending ? "Creating..." : "Create"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export const FormatPartitionDialog = ({
  device,
  isPending,
  error,
  step,
  onClose,
  onConfirm,
}: DialogProps & {
  device: StorageBlockDevice;
  onConfirm: (request: PartitionFormatRequest) => void;
}) => {
  const [fsType, setFsType] = useState("ext4");
  const [label, setLabel] = useState("");
  const [overwrite, setOverwrite] = useState(false);
  const available = useFsAvailable();
  // A signature must be confirmed; without one the bridge still refuses data
  // that udev did not report, and its message points back to this box.
  const valid =
    available(fsType) &&
    device.sizeBytes >= MIN_SIZES[fsType] &&
    labelError(fsType, label) === null &&
    (overwrite || !device.fsType);

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open>
      <AppDialogTitle>{`Format ${device.path}`}</AppDialogTitle>
      <AppDialogContent>
        <div style={stack}>
          <FormatFields
            fsType={fsType}
            label={label}
            onFsType={setFsType}
            onLabel={setLabel}
            sizeBytes={device.sizeBytes}
          />
          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={overwrite}
                onChange={(_, checked) => setOverwrite(checked)}
              />
            }
            label="Overwrite existing data"
          />
          {device.fsType && (
            <AppAlert severity="warning">
              {`${device.path} holds a ${device.fsType} filesystem. Formatting erases it.`}
            </AppAlert>
          )}
          {isPending && <Progress step={step} />}
          {error && <AppAlert severity="error">{error}</AppAlert>}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isPending || !valid}
          onClick={() =>
            onConfirm({ device: device.path, fsType, label, overwrite })
          }
          variant="contained"
        >
          {isPending ? "Formatting..." : "Format"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export const MountPartitionDialog = ({
  device,
  isPending,
  onClose,
  onConfirm,
}: Omit<DialogProps, "step"> & {
  device: StorageBlockDevice;
  onConfirm: (request: PartitionMountRequest) => void;
}) => {
  const [mountpoint, setMountpoint] = useState(
    `/mnt/${device.label || device.name}`,
  );
  const [persist, setPersist] = useState(false);

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open>
      <AppDialogTitle>{`Mount ${device.path}`}</AppDialogTitle>
      <AppDialogContent>
        <div style={stack}>
          <MountFields
            mountpoint={mountpoint}
            onMountpoint={setMountpoint}
            onPersist={setPersist}
            persist={persist}
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isPending || !validMountpoint(mountpoint)}
          onClick={() =>
            onConfirm({ device: device.path, mountpoint, persist })
          }
          variant="contained"
        >
          {isPending ? "Mounting..." : "Mount"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
