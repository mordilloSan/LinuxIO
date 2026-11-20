import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  linuxio,
  type BtrfsFilesystem,
  type BtrfsSpare,
  type BtrfsSpareState,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppConfirmDialog from "@/components/ui/AppConfirmDialog";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formatters";

import { poolDeviceChoices } from "../storageMember";
import { BTRFS_POOL_TOAST_META } from "./btrfsShared";

// The profiles that keep a second copy, so a pool of them runs with a disk
// missing, which is when its hot spare is used (docs/btrfs.md, Hot spare).
const SPARE_PROFILES = ["raid1", "raid1c3", "raid1c4", "raid10"];
const DEFAULT_DELAY = 10;
const MAX_DELAY = 7 * 24 * 60;

const STATES: Record<
  BtrfsSpareState,
  { label: string; color: "success" | "error" | "warning" | "default" }
> = {
  ready: { label: "Ready", color: "success" },
  absent: { label: "Missing", color: "error" },
  "too-small": { label: "Too small", color: "warning" },
  "in-use": { label: "In use", color: "warning" },
  unknown: { label: "Not checked", color: "default" },
};

/** The delay typed, or null unless it is 0 to a week of whole minutes. */
const parseDelay = (text: string) => {
  const minutes = Number(text);
  return text.trim() !== "" &&
    Number.isInteger(minutes) &&
    minutes >= 0 &&
    minutes <= MAX_DELAY
    ? minutes
    : null;
};

const delayText = (minutes: number) =>
  minutes === 0
    ? "Replaces a missing disk at the next health check"
    : `Replaces a disk missing for ${minutesText(minutes)}`;

/** How the spare's disk is recognised: its serial, or its WWN. */
const identity = ({ disk }: BtrfsSpare) =>
  disk.serial ? `Serial ${disk.serial}` : `WWN ${disk.wwn ?? ""}`;

/** A reason the bridge words as a clause, as a sentence. */
const sentence = (clause: string) =>
  `${clause.charAt(0).toUpperCase()}${clause.slice(1)}.`;

const minutesText = (minutes: number) =>
  `${minutes} minute${minutes === 1 ? "" : "s"}`;

/**
 * What LinuxIO does by itself when a disk of the pool goes missing, and when
 * (docs/btrfs.md, "Missing disks" and "Hot spare").
 */
const automaticReplace = (pool: string, on: boolean, spare?: BtrfsSpare) => {
  const onSpare = spare
    ? spare.delayMinutes === 0
      ? "a disk that stays missing is replaced with the hot spare at the next health check"
      : `a disk that stays missing for ${minutesText(spare.delayMinutes)} is replaced with the hot spare`
    : "";
  if (on)
    return `When a disk of ${pool} goes missing and comes back, LinuxIO erases it and rebuilds the pool onto it once two health checks in a row (about 10 minutes) have seen it back${onSpare ? `; ${onSpare}` : ""}.`;
  return `A disk of ${pool} that comes back is taken back only when the pool is mounted again, and ${onSpare || "nothing replaces a missing disk by itself"}.`;
};

/** The pool's switch of the rebuild onto a disk that returns. */
const AutoRebuild = ({
  disabled,
  filesystem,
}: {
  disabled: boolean;
  filesystem: BtrfsFilesystem;
}) => {
  const { uuid, autoRebuild } = filesystem;
  const { mutate, isPending } = linuxio.storage.btrfs_auto_rebuild.useMutation({
    success: "Automatic rebuild changed",
    error: "Failed to change the automatic rebuild",
    toast: BTRFS_POOL_TOAST_META,
  });
  const label = "Rebuild automatically onto a disk that returns";
  return (
    <>
      <div
        style={{
          alignItems: "center",
          display: "flex",
          flexWrap: "wrap",
          gap: "var(--app-space-8)",
        }}
      >
        <Icon
          color="var(--app-palette-text-secondary)"
          height={22}
          icon="mdi:sync"
          width={22}
        />
        <AppTypography variant="body2">{label}</AppTypography>
        <div style={{ marginLeft: "auto" }}>
          <AppSwitch
            aria-label={label}
            checked={autoRebuild}
            disabled={disabled || isPending}
            onChange={(_, enabled) => mutate({ uuid, enabled })}
            size="small"
          />
        </div>
      </div>
      <AppTypography color="text.secondary" variant="caption">
        {automaticReplace(
          filesystem.label || uuid,
          autoRebuild,
          filesystem.spare,
        )}
      </AppTypography>
    </>
  );
};

const DelayField = ({
  onChange,
  value,
}: {
  onChange: (value: string) => void;
  value: string;
}) => (
  <AppTextField
    error={parseDelay(value) === null}
    fullWidth
    helperText="0 replaces at the next health check, which runs every 10 minutes. At most a week."
    label="Delay (minutes)"
    onChange={(event) => onChange(event.target.value)}
    size="small"
    type="number"
    value={value}
  />
);

/** Picks an empty disk no smaller than any of the pool's, and the delay. */
const SetSpareDialog = ({
  filesystem,
  onClose,
}: {
  filesystem: BtrfsFilesystem;
  onClose: () => void;
}) => {
  const { uuid } = filesystem;
  const { data } = useQuery(linuxio.storage.get_topology);
  const [device, setDevice] = useState("");
  const [delay, setDelay] = useState(String(DEFAULT_DELAY));
  const { mutate, isPending } = linuxio.storage.btrfs_spare_set.useMutation({
    success: "Hot spare set",
    error: "Failed to set hot spare",
    toast: BTRFS_POOL_TOAST_META,
  });

  // A replace needs a target no smaller than the disk it replaces.
  const largest = Math.max(0, ...filesystem.devices.map((d) => d.sizeBytes));
  const choices = poolDeviceChoices(
    data?.devices ?? [],
    filesystem.devices.map(({ path }) => path),
    false,
    uuid,
  )
    .filter(({ device: entry }) => entry.type === "disk")
    .map(({ device: entry, reason }) => ({
      device: entry,
      reason:
        reason ??
        (entry.readOnly
          ? "Read-only disk"
          : entry.sizeBytes < largest
            ? "Smaller than a disk of this pool"
            : null),
    }));
  const free = choices.filter(({ reason }) => reason === null);
  const selected = free.some((entry) => entry.device.path === device)
    ? device
    : "";
  const minutes = parseDelay(delay);
  const close = () => {
    if (!isPending) onClose();
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={close} open>
      <AppDialogTitle>Set hot spare</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          {`When a disk of ${filesystem.label || uuid} has been missing for the delay below, LinuxIO replaces it with the hot spare; until then the spare stays empty and nothing else can use it.`}
        </AppDialogContentText>
        <AppSelect
          disabled={isPending}
          fullWidth
          label="Disk"
          onChange={(event) => setDevice(event.target.value)}
          value={selected}
        >
          <option value="">
            {free.length === 0 ? "No empty disks" : "Select a disk"}
          </option>
          {choices.map(({ device: entry, reason }) => (
            <option
              disabled={reason !== null}
              key={entry.path}
              value={entry.path}
            >
              {[
                entry.path,
                formatFileSize(entry.sizeBytes),
                entry.model,
                reason,
              ]
                .filter(Boolean)
                .join(" · ")}
            </option>
          ))}
        </AppSelect>
        <DelayField onChange={setDelay} value={delay} />
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isPending || selected === "" || minutes === null}
          onClick={() => {
            if (minutes === null) return;
            mutate(
              { uuid, device: selected, delayMinutes: minutes },
              { onSuccess: onClose },
            );
          }}
          variant="contained"
        >
          Set hot spare
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

const DelayDialog = ({
  current,
  onClose,
  uuid,
}: {
  current: number;
  onClose: () => void;
  uuid: string;
}) => {
  const [delay, setDelay] = useState(String(current));
  const { mutate, isPending } = linuxio.storage.btrfs_spare_delay.useMutation({
    success: "Hot spare delay changed",
    error: "Failed to change the hot spare delay",
    toast: BTRFS_POOL_TOAST_META,
  });
  const minutes = parseDelay(delay);
  const close = () => {
    if (!isPending) onClose();
  };

  return (
    <GeneralDialog fullWidth maxWidth="xs" onClose={close} open>
      <AppDialogTitle>Hot spare delay</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          How long a disk of this pool is missing before LinuxIO replaces it
          with the hot spare.
        </AppDialogContentText>
        <DelayField onChange={setDelay} value={delay} />
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isPending || minutes === null}
          onClick={() => {
            if (minutes === null) return;
            mutate({ uuid, delayMinutes: minutes }, { onSuccess: onClose });
          }}
          variant="contained"
        >
          Save
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

/**
 * The pool's hot spare on its Disks card: the disk and its state, or why the
 * pool cannot have one, with Set, Change delay and Remove.
 */
const BtrfsHotSpare = ({
  disabled,
  filesystem,
}: {
  disabled: boolean;
  filesystem: BtrfsFilesystem;
}) => {
  const { spare, uuid } = filesystem;
  const pool = filesystem.label || uuid;
  const [dialog, setDialog] = useState<"set" | "delay" | "remove" | null>(null);
  const { mutate: clearSpare, isPending: isClearing } =
    linuxio.storage.btrfs_spare_clear.useMutation({
      success: "Hot spare removed",
      error: "Failed to remove hot spare",
      toast: BTRFS_POOL_TOAST_META,
    });
  const closeDialog = () => setDialog(null);

  const oneCopy = filesystem.allocation.find(
    ({ profile }) => !SPARE_PROFILES.includes(profile),
  );
  const offered = filesystem.allocation.length > 0 && oneCopy === undefined;
  const notOffered = `A hot spare needs raid1, raid1c3, raid1c4 or raid10${
    oneCopy ? `: a ${oneCopy.profile} pool cannot run with a disk missing` : ""
  }.`;
  const state = spare ? STATES[spare.state] : null;

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-4)",
        marginTop: "var(--app-space-12)",
      }}
    >
      <div
        style={{
          alignItems: "center",
          display: "flex",
          flexWrap: "wrap",
          gap: "var(--app-space-8)",
        }}
      >
        <Icon
          color="var(--app-palette-text-secondary)"
          height={22}
          icon="mdi:harddisk"
          width={22}
        />
        <AppTypography variant="body2">Hot spare</AppTypography>
        {spare && state ? (
          <>
            <AppChip
              color={state.color}
              label={state.label}
              size="small"
              variant="soft"
            />
            <AppTypography color="text.secondary" variant="caption">
              {[
                spare.device || "Not connected",
                identity(spare),
                formatFileSize(spare.disk.sizeBytes),
                delayText(spare.delayMinutes),
              ].join(" · ")}
            </AppTypography>
          </>
        ) : (
          <AppTypography color="text.secondary" variant="caption">
            {offered ? "No hot spare" : notOffered}
          </AppTypography>
        )}
        <div
          style={{
            display: "flex",
            gap: "var(--app-space-4)",
            marginLeft: "auto",
          }}
        >
          {spare ? (
            <>
              <AppButton
                disabled={disabled}
                onClick={() => setDialog("delay")}
                size="small"
              >
                Change delay
              </AppButton>
              <AppButton
                disabled={disabled}
                onClick={() => setDialog("remove")}
                size="small"
              >
                Remove hot spare
              </AppButton>
            </>
          ) : (
            offered && (
              <AppButton
                disabled={disabled}
                onClick={() => setDialog("set")}
                size="small"
                variant="outlined"
              >
                Set hot spare
              </AppButton>
            )
          )}
        </div>
      </div>
      {spare && spare.state !== "ready" && spare.reason && (
        <AppTypography color="text.secondary" variant="caption">
          {sentence(spare.reason)}
        </AppTypography>
      )}
      {spare && !offered && (
        <AppTypography color="text.secondary" variant="caption">
          {notOffered}
        </AppTypography>
      )}
      {offered && <AutoRebuild disabled={disabled} filesystem={filesystem} />}

      {dialog === "set" && (
        <SetSpareDialog filesystem={filesystem} onClose={closeDialog} />
      )}
      {dialog === "delay" && spare && (
        <DelayDialog
          current={spare.delayMinutes}
          onClose={closeDialog}
          uuid={uuid}
        />
      )}
      <AppConfirmDialog
        confirmText="Remove"
        isPending={isClearing}
        message={
          spare
            ? `Remove ${spare.device || "the missing disk"} as the hot spare of ${pool}? The disk is free again, and a missing disk is no longer replaced by itself.`
            : ""
        }
        onClose={closeDialog}
        onConfirm={() => clearSpare({ uuid }, { onSuccess: closeDialog })}
        open={dialog === "remove"}
        title="Remove hot spare"
      />
    </div>
  );
};

export default BtrfsHotSpare;
