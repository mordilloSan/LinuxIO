import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo, useState, type MouseEvent } from "react";

import MountCard, {
  type MountBase,
  type MountListQuery,
} from "@/components/cards/MountCard";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { getUsageColor } from "@/constants/statusColors";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { CARD_GRID_SIZE_STANDARD } from "@/theme/constants";
import { formatFileSize } from "@/utils/formaters";

const MOUNTS_REFETCH_MS = 10_000;

type PendingMountAction = "mount" | "unmount";

export interface ProtocolMountListProps<T extends MountBase> {
  ariaLabel: string;
  /** Status chips shown in the table, the card and the expanded row. */
  chips: (mount: T) => string[];
  emptyMessage: string;
  listQueryOptions: MountListQuery<T>;
  /** When set, mounting is disabled and this is the tooltip. */
  mountUnavailableReason?: string;
  onEdit: (mount: T) => void;
  /** Return the mutation promise to show progress; return nothing to skip. */
  onMount: (mount: T) => Promise<unknown> | undefined;
  onRemove: (mount: T) => void;
  onUnmount: (mount: T) => Promise<unknown> | undefined;
  persistExpandedKey: string;
  reorderAriaLabel: string;
  sourceHeader: string;
  surfaceId: string;
  viewMode: "table" | "card";
}

interface MountActionProps<T extends MountBase> {
  chips: (mount: T) => string[];
  mountUnavailableReason?: string;
  onEdit: (mount: T) => void;
  onMount: (mount: T) => void;
  onRemove: (mount: T) => void;
  onUnmount: (mount: T) => void;
  pendingActionByMountpoint: ReadonlyMap<string, PendingMountAction>;
}

const getMountId = (mount: MountBase) => mount.mountpoint;
const identity = (mountpoint: string) => mountpoint;
const selectMountIdentities = (mounts: MountBase[]) =>
  mounts.map((mount) => mount.mountpoint);

const MountEntryActions = <T extends MountBase>({
  mount,
  mountUnavailableReason,
  onEdit,
  onMount,
  onRemove,
  onUnmount,
  pendingActionByMountpoint,
  stopPropagation = false,
}: Omit<MountActionProps<T>, "chips"> & {
  mount: T;
  stopPropagation?: boolean;
}) => {
  const wrapClick =
    (handler: (mount: T) => void) => (event: MouseEvent<HTMLButtonElement>) => {
      if (stopPropagation) event.stopPropagation();
      handler(mount);
    };

  const pendingAction = pendingActionByMountpoint.get(mount.mountpoint);
  const isPending = Boolean(pendingAction);
  const mountActionDisabled = !mount.mounted && Boolean(mountUnavailableReason);
  const mountActionLabel = mount.mounted ? "Unmount entry" : "Mount entry";
  const mountActionTitle = mountActionDisabled
    ? mountUnavailableReason
    : pendingAction
      ? pendingAction === "mount"
        ? "Mounting..."
        : "Unmounting..."
      : mountActionLabel;

  return (
    <div
      aria-busy={isPending}
      aria-label={`Actions for ${mount.mountpoint}`}
      role="group"
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
        disabled={isPending}
        icon="mdi:pencil-outline"
        iconSize={18}
        label="Edit entry"
        onClick={wrapClick(onEdit)}
      />
      <AppActionIconButton
        ariaLabel={
          pendingAction
            ? `${pendingAction === "mount" ? "Mounting" : "Unmounting"} ${mount.mountpoint}`
            : mountActionLabel
        }
        color={
          mount.mounted
            ? "var(--app-palette-success-main)"
            : "var(--app-palette-text-secondary)"
        }
        disabled={isPending || mountActionDisabled}
        icon={mount.mounted ? "mdi:link-variant" : "mdi:link-variant-off"}
        iconSize={18}
        label={mountActionTitle}
        loading={isPending}
        onClick={wrapClick(mount.mounted ? onUnmount : onMount)}
      />
      <AppActionIconButton
        ariaLabel="Remove entry"
        color="var(--app-palette-error-main)"
        disabled={isPending}
        icon="mdi:trash-can-outline"
        iconSize={18}
        label="Remove entry"
        onClick={wrapClick(onRemove)}
      />
    </div>
  );
};

const MountCardGrid = <T extends MountBase>({
  chips,
  emptyMessage,
  listQueryOptions,
  surfaceId,
  ...actionProps
}: MountActionProps<T> & {
  emptyMessage: string;
  listQueryOptions: MountListQuery<T>;
  surfaceId: string;
}) => {
  const { data: mountpoints } = useSuspenseQuery({
    ...listQueryOptions,
    refetchInterval: MOUNTS_REFETCH_MS,
    select: selectMountIdentities,
  });
  // Cards and rows key the same surface: both identify a mount by mountpoint,
  // so a manual order set in one view shows up in the other.
  const surface = useReorderableSurface({
    getId: identity,
    items: mountpoints,
    surface: surfaceId,
  });

  if (mountpoints.length === 0) {
    return (
      <div style={{ textAlign: "center", paddingBlock: 16 }}>
        <AppTypography color="text.secondary" variant="body2">
          {emptyMessage}
        </AppTypography>
      </div>
    );
  }

  const renderActions = (mount: T) => (
    <MountEntryActions mount={mount} {...actionProps} />
  );

  return (
    <ReorderableCardGrid
      fillAvailable={false}
      getId={identity}
      renderItem={(mountpoint) => (
        <MountCard
          actions={renderActions}
          chips={chips}
          listQueryOptions={listQueryOptions}
          mountpoint={mountpoint}
        />
      )}
      size={CARD_GRID_SIZE_STANDARD}
      surface={surface}
    />
  );
};

const MountTable = <T extends MountBase>({
  ariaLabel,
  chips,
  emptyMessage,
  listQueryOptions,
  persistExpandedKey,
  reorderAriaLabel,
  sourceHeader,
  surfaceId,
  mountUnavailableReason,
  onEdit,
  onMount,
  onRemove,
  onUnmount,
  pendingActionByMountpoint,
}: MountActionProps<T> & {
  ariaLabel: string;
  emptyMessage: string;
  listQueryOptions: MountListQuery<T>;
  persistExpandedKey: string;
  reorderAriaLabel: string;
  sourceHeader: string;
  surfaceId: string;
}) => {
  const { data: mounts } = useSuspenseQuery({
    ...listQueryOptions,
    refetchInterval: MOUNTS_REFETCH_MS,
  });
  const surface = useReorderableSurface({
    getId: getMountId,
    items: mounts,
    surface: surfaceId,
  });
  const tableDnd = useReorderableTableDnd<T, T>({
    handleAriaLabel: reorderAriaLabel,
    surface,
  });
  const columns = useMemo<AppVirtualTableColumnDef<T>[]>(
    () => [
      {
        accessorKey: "source",
        header: sourceHeader,
        cell: ({ row }) => (
          <AppTypography
            style={{ fontFamily: "var(--app-font-mono)" }}
            variant="body2"
          >
            {row.original.source}
          </AppTypography>
        ),
        meta: {
          align: "left",
          getCellRenderKey: (row) => {
            const mount = row as T;
            return [mount.mountpoint, mount.source];
          },
        },
      },
      {
        accessorKey: "mountpoint",
        header: "Mount Point",
        cell: ({ row }) => (
          <AppTypography
            style={{ fontFamily: "var(--app-font-mono)" }}
            variant="body2"
          >
            {row.original.mountpoint}
          </AppTypography>
        ),
        meta: {
          align: "left",
          getCellRenderKey: (row) => (row as T).mountpoint,
        },
      },
      {
        id: "status",
        header: "Status",
        accessorFn: (mount) => chips(mount).join(" "),
        cell: ({ row }) => (
          <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
            {chips(row.original).map((label) => (
              <Chip key={label} label={label} size="small" variant="soft" />
            ))}
          </div>
        ),
        meta: {
          align: "left",
          getCellRenderKey: (row) => {
            const mount = row as T;
            return [mount.mountpoint, ...chips(mount)];
          },
          width: "200px",
        },
      },
      {
        accessorKey: "usedPct",
        header: "Usage",
        cell: ({ row }) => {
          const mount = row.original;
          return mount.mounted ? (
            <div style={{ width: "100%" }}>
              <AppLinearProgress
                color={getUsageColor(mount.usedPct)}
                style={{ height: 6, borderRadius: 3, marginBottom: 2 }}
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
          getCellRenderKey: (row) => {
            const mount = row as T;
            return [
              mount.mountpoint,
              mount.mounted,
              mount.usedPct,
              mount.used,
              mount.size,
            ];
          },
          hideBelow: "sm",
          width: "200px",
        },
      },
      {
        id: "actions",
        header: "Actions",
        enableSorting: false,
        cell: ({ row }) => (
          <MountEntryActions
            mount={row.original}
            mountUnavailableReason={mountUnavailableReason}
            onEdit={onEdit}
            onMount={onMount}
            onRemove={onRemove}
            onUnmount={onUnmount}
            pendingActionByMountpoint={pendingActionByMountpoint}
            stopPropagation
          />
        ),
        meta: {
          align: "right",
          getCellRenderKey: (row) => {
            const mount = row as T;
            return [
              mount.mountpoint,
              mount.mounted,
              pendingActionByMountpoint.get(mount.mountpoint),
              mountUnavailableReason,
            ];
          },
          width: "160px",
        },
      },
    ],
    [
      chips,
      mountUnavailableReason,
      onEdit,
      onMount,
      onRemove,
      onUnmount,
      pendingActionByMountpoint,
      sourceHeader,
    ],
  );

  return (
    <AppVirtualTable
      ariaLabel={ariaLabel}
      columns={columns}
      data={surface.items}
      dnd={tableDnd}
      emptyMessage={emptyMessage}
      fillAvailable={false}
      getRowId={getMountId}
      maxHeight={400}
      persistExpandedKey={persistExpandedKey}
      renderExpandedContent={({ original: mount }) => (
        <div className="expand-panel">
          <AppTypography gutterBottom variant="subtitle2">
            <strong>Status:</strong> {chips(mount).join(" / ")}
          </AppTypography>
          <div>
            <AppTypography gutterBottom variant="subtitle2">
              <strong>Options:</strong>
            </AppTypography>
            <div className="expand-panel__chips">
              {mount.options && mount.options.length > 0 ? (
                mount.options.map((option, index) => (
                  <Chip
                    key={index}
                    label={option}
                    size="small"
                    variant="soft"
                  />
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
              {formatFileSize(mount.size)} ({mount.usedPct.toFixed(1)}% used,{" "}
              {formatFileSize(mount.free)} free)
            </AppTypography>
          ) : (
            <AppTypography gutterBottom variant="subtitle2">
              <strong>Storage:</strong> Not currently mounted
            </AppTypography>
          )}
        </div>
      )}
    />
  );
};

/**
 * Table or card list for one mount protocol (NFS, SMB). Owns per-entry
 * mount/unmount progress; the caller owns the dialogs and mutations.
 */
const ProtocolMountList = <T extends MountBase>({
  onMount,
  onUnmount,
  viewMode,
  ...rest
}: ProtocolMountListProps<T>) => {
  const [pendingActionByMountpoint, setPendingActionByMountpoint] = useState<
    ReadonlyMap<string, PendingMountAction>
  >(() => new Map());

  const runMountAction = (
    mount: T,
    action: PendingMountAction,
    run: (mount: T) => Promise<unknown> | undefined,
  ) => {
    if (pendingActionByMountpoint.has(mount.mountpoint)) return;
    const promise = run(mount);
    if (!promise) return;

    setPendingActionByMountpoint((current) =>
      new Map(current).set(mount.mountpoint, action),
    );
    void promise
      .catch(() => undefined)
      .finally(() => {
        setPendingActionByMountpoint((current) => {
          if (current.get(mount.mountpoint) !== action) return current;
          const next = new Map(current);
          next.delete(mount.mountpoint);
          return next;
        });
      });
  };

  const actionProps = {
    ...rest,
    onMount: (mount: T) => runMountAction(mount, "mount", onMount),
    onUnmount: (mount: T) => runMountAction(mount, "unmount", onUnmount),
    pendingActionByMountpoint,
  };

  return viewMode === "card" ? (
    <MountCardGrid {...actionProps} />
  ) : (
    <MountTable {...actionProps} />
  );
};

export default ProtocolMountList;
