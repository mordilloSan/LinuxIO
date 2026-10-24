import { Icon } from "@iconify/react";
import { useState } from "react";

import { linuxio, useCallMutation, type MDArray, type MDMember } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppChip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import useAuth from "@/hooks/useAuth";
import { formatFileSize } from "@/utils/formatters";

import { deviceTypeLabel } from "./storagePools";
import BtrfsAddDeviceDialog from "../btrfs/BtrfsAddDeviceDialog";
import { BTRFS_POOL_TOAST_META } from "../btrfs/btrfsShared";

type RaidDialog =
  | { kind: "add" }
  | { kind: "fail"; member: MDMember }
  | { kind: "remove"; member: MDMember }
  | { kind: "replace"; member: MDMember };

/** md/sync_action while it runs; "idle" and "frozen" are not shown. */
const SYNC_LABEL: Record<string, string> = {
  check: "Checking",
  repair: "Repairing",
  resync: "Resyncing",
  recover: "Rebuilding",
  reshape: "Reshaping",
};

/** md/dev-X/state words as people read them; others are shown as reported. */
const MEMBER_STATE: Record<string, string> = {
  in_sync: "In sync",
  faulty: "Failed",
  spare: "Spare",
  want_replacement: "Being replaced",
  replacement: "Replacement",
  write_error: "Write errors",
  blocked: "Blocked",
  writemostly: "Write-mostly",
};

const inSync = (member: MDMember) => member.state.includes("in_sync");
const removable = (member: MDMember) =>
  member.slot < 0 ||
  member.state.includes("faulty") ||
  member.state.includes("spare");

const toast = { toast: BTRFS_POOL_TOAST_META };

/** An md RAID array's health, scrub and member disks, with their operations. */
const MdRaidDetails = ({ array }: { array: MDArray }) => {
  const { privileged } = useAuth();
  const [dialog, setDialog] = useState<RaidDialog | null>(null);
  const closeDialog = () => setDialog(null);
  const { mutate: setSyncAction, isPending: isSetting } = useCallMutation(
    linuxio.storage.md_sync_action,
    { error: "Failed to change the RAID check", ...toast },
  );
  const { mutate: failMember, isPending: isFailing } = useCallMutation(
    linuxio.storage.md_member_fail,
    { success: "Disk marked failed", error: "Failed to fail disk", ...toast },
  );
  const { mutate: removeMember, isPending: isRemoving } = useCallMutation(
    linuxio.storage.md_member_remove,
    { success: "Disk removed", error: "Failed to remove disk", ...toast },
  );
  const { mutate: addMember, isPending: isAdding } = useCallMutation(
    linuxio.storage.md_member_add,
    { success: "Disk added as a spare", error: "Failed to add disk", ...toast },
  );
  const { mutate: replaceMember, isPending: isReplacing } = useCallMutation(
    linuxio.storage.md_member_replace,
    {
      success: "Replacement started",
      error: "Failed to start replacement",
      ...toast,
    },
  );
  const pending =
    isSetting || isFailing || isRemoving || isAdding || isReplacing;
  const { name } = array;
  const syncing = SYNC_LABEL[array.syncAction];
  const scrubbing =
    array.syncAction === "check" || array.syncAction === "repair";
  const synced = array.members.filter(inSync).length;
  const memberPaths = array.members.map(({ path }) => path);

  const columns: AppVirtualTableColumnDef<MDMember>[] = [
    {
      accessorKey: "path",
      header: "Disk",
      cell: ({ row }) => {
        const { path, state } = row.original;
        const failed = state.includes("faulty");
        return (
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <StatusDot
              color={
                failed
                  ? "var(--app-palette-error-main)"
                  : inSync(row.original)
                    ? "var(--app-palette-success-main)"
                    : "var(--app-palette-warning-main)"
              }
              tooltip={
                failed ? "Failed" : inSync(row.original) ? "OK" : "Not in sync"
              }
            />
            <Icon
              color="var(--app-palette-text-secondary)"
              height={22}
              icon="mdi:harddisk"
              width={22}
            />
            {path}
          </div>
        );
      },
    },
    {
      id: "state",
      header: "State",
      cell: ({ row }) => (
        <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
          {row.original.state.map((word) => (
            <AppChip
              color={
                word === "faulty" || word === "write_error"
                  ? "error"
                  : word === "in_sync"
                    ? "success"
                    : "warning"
              }
              key={word}
              label={MEMBER_STATE[word] ?? word}
              size="small"
              variant="soft"
            />
          ))}
        </div>
      ),
    },
    {
      accessorKey: "slot",
      header: "Slot",
      cell: ({ row }) => (
        <AppTypography color="text.secondary" variant="caption">
          {row.original.slot < 0 ? "—" : row.original.slot}
        </AppTypography>
      ),
      meta: { align: "right" },
    },
    {
      accessorKey: "errors",
      header: "Read errors",
      cell: ({ row }) => (
        <AppTypography
          color={row.original.errors > 0 ? "error" : "text.secondary"}
          variant="caption"
        >
          {row.original.errors > 0 ? row.original.errors : "—"}
        </AppTypography>
      ),
      meta: { align: "right" },
    },
    ...(privileged
      ? [
          {
            id: "actions",
            header: "Actions",
            cell: ({ row }) => (
              <div
                style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}
              >
                {inSync(row.original) && (
                  <>
                    <AppActionIconButton
                      disabled={pending}
                      icon="mdi:swap-horizontal"
                      label={`Replace ${row.original.path}`}
                      onClick={() =>
                        setDialog({ kind: "replace", member: row.original })
                      }
                    />
                    <AppActionIconButton
                      disabled={pending}
                      icon="mdi:close-octagon-outline"
                      label={`Mark ${row.original.path} failed`}
                      onClick={() =>
                        setDialog({ kind: "fail", member: row.original })
                      }
                    />
                  </>
                )}
                {removable(row.original) && (
                  <AppActionIconButton
                    disabled={pending}
                    icon="mdi:delete"
                    label={`Remove ${row.original.path}`}
                    onClick={() =>
                      setDialog({ kind: "remove", member: row.original })
                    }
                  />
                )}
              </div>
            ),
            meta: { align: "right" },
          } satisfies AppVirtualTableColumnDef<MDMember>,
        ]
      : []),
  ];

  const confirm =
    dialog?.kind === "fail"
      ? {
          title: "Mark disk failed",
          text: `Mark ${dialog.member.path} failed? ${array.path} keeps running on its other disks, without this one's redundancy, until the disk is removed and replaced.`,
          label: "Mark failed",
          run: () => failMember({ name, member: dialog.member.name }),
        }
      : dialog?.kind === "remove"
        ? {
            title: "Remove disk",
            text: `Remove ${dialog.member.path} from ${array.path}? Its data is no longer kept in sync.`,
            label: "Remove",
            run: () => removeMember({ name, member: dialog.member.name }),
          }
        : null;

  return (
    <>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 8,
        }}
      >
        <AppTypography style={{ fontWeight: 600 }} variant="h6">
          Details · {array.path}
        </AppTypography>
        {privileged && array.syncAction !== "" && (
          <div style={{ display: "flex", gap: 8 }}>
            {scrubbing ? (
              <AppButton
                disabled={pending}
                onClick={() => setSyncAction({ name, action: "idle" })}
                size="small"
                variant="outlined"
              >
                Stop
              </AppButton>
            ) : (
              <>
                <AppButton
                  disabled={pending || Boolean(syncing)}
                  onClick={() => setSyncAction({ name, action: "check" })}
                  size="small"
                  variant="outlined"
                >
                  Check
                </AppButton>
                <AppButton
                  disabled={pending || Boolean(syncing)}
                  onClick={() => setSyncAction({ name, action: "repair" })}
                  size="small"
                  variant="outlined"
                >
                  Repair
                </AppButton>
              </>
            )}
          </div>
        )}
      </div>
      <FrostedCard style={{ marginBottom: 16 }}>
        <AppCardContent>
          <CardIconHeader
            iconTint
            icon={<Icon height={24} icon="mdi:layers-outline" width={24} />}
            right={
              privileged && (
                <AppActionIconButton
                  disabled={pending}
                  icon="mdi:plus"
                  label="Add disk"
                  onClick={() => setDialog({ kind: "add" })}
                />
              )
            }
            subtitle={`${synced} of ${array.raidDisks} disks in sync`}
            title={deviceTypeLabel(array.level)}
          />
          <div
            style={{
              display: "flex",
              flexWrap: "wrap",
              alignItems: "center",
              gap: 8,
              marginBottom: 12,
            }}
          >
            <AppChip
              color={array.degraded > 0 ? "error" : "success"}
              label={
                array.degraded > 0
                  ? `Degraded (${array.degraded} missing)`
                  : "Healthy"
              }
              size="small"
              variant="soft"
            />
            <AppTypography color="text.secondary" variant="caption">
              {[
                formatFileSize(array.sizeBytes),
                `state ${array.state}`,
                array.metadataVersion && `metadata ${array.metadataVersion}`,
                array.syncAction !== "" &&
                  `${array.mismatchCount} mismatched sectors at the last check`,
              ]
                .filter(Boolean)
                .join(" · ")}
            </AppTypography>
          </div>
          {syncing && (
            <div style={{ marginBottom: 12 }}>
              <AppLinearProgress
                aria-label={`${array.path} ${syncing.toLowerCase()}`}
                value={array.syncProgress}
                variant="determinate"
              />
              <AppTypography color="text.secondary" variant="caption">
                {`${syncing} · ${array.syncProgress.toFixed(1)}%`}
                {array.syncSpeedBytes > 0 &&
                  ` · ${formatFileSize(array.syncSpeedBytes)}/s`}
              </AppTypography>
            </div>
          )}
          <AppVirtualTable
            ariaLabel="RAID disks"
            columns={columns}
            data={array.members}
            density="compact"
            emptyMessage="No disks found"
            fillAvailable={false}
            getRowId={(member) => member.name}
            maxHeight={320}
            variant="embedded"
          />
        </AppCardContent>
      </FrostedCard>

      {dialog?.kind === "add" && (
        <BtrfsAddDeviceDialog
          confirmLabel="Add"
          filesystemDevices={memberPaths}
          isPending={isAdding}
          label="Device"
          onClose={closeDialog}
          onConfirm={(device) =>
            addMember({ name, device }, { onSuccess: closeDialog })
          }
          title={`Add a spare to ${array.path}`}
        />
      )}
      {dialog?.kind === "replace" && (
        <BtrfsAddDeviceDialog
          confirmLabel="Replace"
          filesystemDevices={memberPaths}
          isPending={isReplacing}
          label="New device"
          onClose={closeDialog}
          onConfirm={(device) =>
            replaceMember(
              { name, member: dialog.member.name, device },
              { onSuccess: closeDialog },
            )
          }
          title={`Replace ${dialog.member.path}`}
        />
      )}
      <GeneralDialog
        fullWidth
        maxWidth="sm"
        onClose={closeDialog}
        open={confirm !== null}
      >
        <AppDialogTitle>{confirm?.title}</AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>{confirm?.text}</AppDialogContentText>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={closeDialog}>Cancel</AppButton>
          <AppButton
            color="error"
            onClick={() => {
              confirm?.run();
              closeDialog();
            }}
            variant="contained"
          >
            {confirm?.label}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </>
  );
};

export default MdRaidDetails;
