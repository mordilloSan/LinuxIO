import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import {
  useRef,
  useState,
  type DragEvent,
  type MouseEvent,
  type ReactNode,
  type RefObject,
} from "react";

import {
  getMutationErrorMessage,
  linuxio,
  type TrashEntry,
  type TrashRestoreRequest,
} from "@/api";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppConfirmDialog from "@/components/ui/AppConfirmDialog";
import AppDivider from "@/components/ui/AppDivider";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppSkeleton from "@/components/ui/AppSkeleton";
import HeaderActions from "@/components/ui/HeaderActions";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useLatestRef } from "@/hooks/useLatestRef";
import { allowContextMenuProps } from "@/utils/contextMenu";
import { stripTrailingSlash } from "@/utils/path";

import { useFileListKeyboardNavigation } from "../-hooks/useFileListKeyboardNavigation";
import { useShownCardSize } from "../-hooks/useShownCardSize";
import type { TrashJobs } from "../-hooks/useTrashJobs";
import {
  acceptInternalDrop,
  hasInternalDrag,
  refuseFileDrop,
} from "../-internalDrag";
import {
  nextTrashSort,
  sortTrashEntries,
  type TrashSort,
  trashShownName,
} from "../-trashEntries";
import type { ViewMode } from "../-types";
import CardSizeSlider from "./CardSizeSlider";
import EmptyState from "./EmptyState";
import TrashDetailsDialog from "./TrashDetailsDialog";
import TrashList from "./TrashList";
import TrashRestoreToDialog from "./TrashRestoreToDialog";

interface TrashPlaceProps {
  /** The trail: the folders the Trash was opened from, then the Trash. */
  breadcrumbs: ReactNode;
  /** The card view's size step, the folder listing's setting. */
  cardSize: number;
  /** The folder the Trash was opened from; Restore to… starts there. */
  folderPath: string;
  jobs: TrashJobs;
  onCardSizeChange: (size: number) => void;
  /** Back to the folder the Trash was opened from. */
  onLeave: () => void;
  /** The sort the user picked, which the page keeps in the URL. */
  onSortChange: (sort: TrashSort) => void;
  /** Switches between cards and the list, the folder listing's setting. */
  onSwitchView: () => void;
  /** Moves items dragged here from a folder to the Trash. */
  onTrashItems: (paths: string[]) => void;
  sort: TrashSort;
  viewMode: ViewMode;
}

type ConfirmKind = "delete" | "empty" | "restore";

const itemCount = (count: number) => `${count} item${count === 1 ? "" : "s"}`;

const subject = (entries: readonly TrashEntry[]) =>
  entries.length === 1 ? trashShownName(entries[0]) : itemCount(entries.length);

const confirmText = (kind: ConfirmKind, entries: readonly TrashEntry[]) => {
  switch (kind) {
    case "delete":
      return {
        confirm: "Delete",
        message: `Permanently delete ${subject(entries)}? This cannot be undone.`,
        title: "Delete permanently",
      };
    case "empty":
      return {
        confirm: "Empty Trash",
        message: `Permanently delete everything in the Trash (${itemCount(entries.length)})? This cannot be undone.`,
        title: "Empty Trash",
      };
    case "restore": {
      // Items another program trashed carry no record of where they came
      // from, so a restore into a folder the user does not own names its
      // destination and asks first; the server refuses it unconfirmed.
      const destinations = entries
        .filter((entry) => entry.confirmRestore && entry.originalPath)
        .map((entry) => entry.originalPath);
      const one = destinations.length === 1;
      return {
        confirm: "Restore",
        message: `Restore to ${destinations.join(", ")}? Another program moved ${one ? "this item" : "these items"} to the Trash, so LinuxIO cannot vouch for where ${one ? "it" : "they"} came from.`,
        title: "Restore to a system folder",
      };
    }
  }
};

/**
 * The Trash as a place in the file browser, as the Windows Recycle Bin is a
 * window: the items of every trash the user has, newest deleted first, with
 * where each came from. Restore, Restore to…, Delete permanently and Empty
 * Trash are jobs: each starts at once, any number run beside each other, the
 * task tray shows them, and each reports its own result.
 */
const noItems: never[] = [];
const noSelection = new Set<string>();
const ignore = () => {};

// Backspace and Alt+Up lead back to the folders while the list is not shown
// (an empty Trash, loading, a failed read): the list handles them otherwise.
const LeaveKeys = ({ onLeave }: { onLeave: () => void }) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const anchorIndexRef = useRef(-1);
  useFileListKeyboardNavigation({
    allItems: noItems,
    anchorIndexRef,
    containerRef: containerRef as RefObject<HTMLDivElement>,
    focusedIndex: -1,
    global: true,
    onFocusChange: ignore,
    onNavigateUp: onLeave,
    onSelectionChange: ignore,
    selectedPaths: noSelection,
  });
  return null;
};

const TrashPlace = ({
  breadcrumbs,
  cardSize,
  folderPath,
  jobs,
  onCardSizeChange,
  onLeave,
  onSortChange,
  onSwitchView,
  onTrashItems,
  sort,
  viewMode,
}: TrashPlaceProps) => {
  const { data, error, isLoading, refetch } = useQuery(
    linuxio.filebrowser.trash_list,
  );
  // The list fills the area under the header, in an empty Trash too: the
  // slider is offered where a step can change the cards there.
  const listAreaRef = useRef<HTMLDivElement>(null);
  const shownCardSize = useShownCardSize(listAreaRef, cardSize);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [menuPosition, setMenuPosition] = useState<{
    left: number;
    top: number;
  } | null>(null);
  // Each dialog keeps what it shows while it fades out after closing, and
  // acts only while it is open: a second click during the fade does not
  // start its job again.
  const [details, setDetails] = useState<{ open: boolean; paths: string[] }>({
    open: false,
    paths: [],
  });
  const [restoreTo, setRestoreTo] = useState<{
    entries: TrashEntry[];
    open: boolean;
    opened: number;
  }>({ entries: [], open: false, opened: 0 });
  const [confirm, setConfirm] = useState<{
    entries: TrashEntry[];
    kind: ConfirmKind;
    open: boolean;
  }>({ entries: [], kind: "delete", open: false });
  const detailsOpenRef = useLatestRef(details.open);
  const restoreToOpenRef = useLatestRef(restoreTo.open);
  const confirmOpenRef = useLatestRef(confirm.open);
  const [dropTarget, setDropTarget] = useState(false);

  const entries = sortTrashEntries(data?.entries ?? [], sort.field, sort.order);
  // Derived from the live list, so items that left the Trash leave the
  // selection with it.
  const selectedEntries = entries.filter((entry) => selected.has(entry.path));
  const selectedPaths = new Set(selectedEntries.map((entry) => entry.path));
  const detailEntries = entries.filter((entry) =>
    details.paths.includes(entry.path),
  );

  // A job's items leave the selection as it starts, so a second click
  // cannot send them again while it runs.
  const deselect = (paths: readonly string[]) =>
    setSelected((previous) => {
      const next = new Set(previous);
      for (const path of paths) next.delete(path);
      return next;
    });
  const closeMenu = () => setMenuPosition(null);

  const startRestore = (targets: TrashEntry[], confirmed: boolean) => {
    const paths = targets.map((entry) => entry.path);
    const request: TrashRestoreRequest = { paths };
    if (confirmed) {
      // Each destination is confirmed for its own item only.
      request.confirmedDestinations = Object.fromEntries(
        targets
          .filter((entry) => entry.confirmRestore && entry.originalPath)
          .map((entry) => [entry.path, entry.originalPath]),
      );
    }
    deselect(paths);
    jobs.restore(request, targets);
  };

  const restore = (targets: TrashEntry[]) => {
    closeMenu();
    if (targets.length === 0) return;
    if (targets.some((entry) => entry.confirmRestore && entry.originalPath)) {
      setConfirm({ entries: targets, kind: "restore", open: true });
      return;
    }
    startRestore(targets, false);
  };
  const askRestoreTo = (targets: TrashEntry[]) => {
    closeMenu();
    if (targets.length === 0) return;
    setRestoreTo((previous) => ({
      entries: targets,
      open: true,
      opened: previous.opened + 1,
    }));
  };
  const askDelete = (targets: TrashEntry[]) => {
    closeMenu();
    if (targets.length === 0) return;
    setConfirm({ entries: targets, kind: "delete", open: true });
  };
  const askEmpty = () => {
    closeMenu();
    if (entries.length === 0) return;
    setConfirm({ entries, kind: "empty", open: true });
  };
  const showDetails = (paths: string[]) => {
    closeMenu();
    if (paths.length > 0) setDetails({ open: true, paths });
  };

  const confirmAction = () => {
    if (!confirmOpenRef.current) return;
    setConfirm((previous) => ({ ...previous, open: false }));
    if (confirm.kind === "restore") {
      startRestore(confirm.entries, true);
      return;
    }
    const paths = confirm.entries.map((entry) => entry.path);
    deselect(paths);
    jobs.purge(paths);
  };
  const confirmRestoreTo = (destination: string) => {
    if (!restoreToOpenRef.current) return;
    setRestoreTo((previous) => ({ ...previous, open: false }));
    const targets = restoreTo.entries;
    const paths = targets.map((entry) => entry.path);
    deselect(paths);
    // A restore into a chosen folder goes under the name each item is shown
    // with; it is the user's choice, so nothing is confirmed for it.
    jobs.restore({
      destination: stripTrailingSlash(destination),
      names: Object.fromEntries(
        targets.map((entry) => [entry.path, trashShownName(entry)]),
      ),
      paths,
    });
  };
  // The properties dialog closes as it hands its items to an action.
  const fromDetails =
    (action: (targets: TrashEntry[]) => void) => (targets: TrashEntry[]) => {
      if (!detailsOpenRef.current) return;
      setDetails((previous) => ({ ...previous, open: false }));
      action(targets);
    };

  const handleContextMenu = (event: MouseEvent) => {
    event.preventDefault();
    setMenuPosition({ left: event.clientX, top: event.clientY });
  };

  // Items dragged here from a folder, in this tab or another, go to the
  // Trash. Items dragged out of the Trash carry another type and are left
  // alone.
  const dropProps = {
    onDragLeave: (event: DragEvent) => {
      if (event.currentTarget.contains(event.relatedTarget as Node | null)) {
        return;
      }
      setDropTarget(false);
    },
    onDragOver: (event: DragEvent) => {
      // Desktop files are not taken here, nor opened by the browser.
      if (!hasInternalDrag(event.dataTransfer)) {
        refuseFileDrop(event);
        return;
      }
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
      setDropTarget(true);
    },
    onDrop: (event: DragEvent) => {
      if (!hasInternalDrag(event.dataTransfer)) {
        refuseFileDrop(event);
        return;
      }
      event.preventDefault();
      setDropTarget(false);
      const paths = acceptInternalDrop(event.dataTransfer);
      if (paths.length > 0) onTrashItems(paths);
    },
  };

  const hasSelection = selectedEntries.length > 0;
  const actions = (
    <HeaderActions
      maintenance={
        <AppActionIconButton
          disabled={entries.length === 0}
          icon="mdi:delete-sweep-outline"
          iconSize={20}
          label="Empty Trash"
          onClick={askEmpty}
        />
      }
      options={
        <>
          <AppActionIconButton
            disabled={!hasSelection}
            icon="mdi:restore"
            iconSize={20}
            label="Restore"
            onClick={() => restore(selectedEntries)}
          />
          <AppActionIconButton
            disabled={!hasSelection}
            icon="mdi:folder-move-outline"
            iconSize={20}
            label="Restore to…"
            onClick={() => askRestoreTo(selectedEntries)}
          />
          <AppActionIconButton
            disabled={!hasSelection}
            icon="mdi:delete-forever-outline"
            iconSize={20}
            label="Delete permanently"
            onClick={() => askDelete(selectedEntries)}
          />
        </>
      }
      refresh={
        <AppActionIconButton
          icon="mdi:refresh"
          iconSize={20}
          label="Refresh"
          onClick={() => void refetch()}
        />
      }
      view={
        <>
          {viewMode === "card" && shownCardSize.offered ? (
            <CardSizeSlider onChange={onCardSizeChange} value={cardSize} />
          ) : null}
          <ViewModeToggle
            alternateMode="list"
            onViewModeChange={onSwitchView}
            viewMode={viewMode}
          />
        </>
      }
    />
  );

  let body: ReactNode;
  let listShown = false;
  if (isLoading) {
    body = (
      <div
        aria-busy="true"
        style={{ display: "flex", flexDirection: "column", gap: 4 }}
      >
        {[0, 1, 2].map((row) => (
          <AppSkeleton height={40} key={row} variant="rectangular" />
        ))}
      </div>
    );
  } else if (error) {
    body = (
      <AppAlert
        action={
          <AppButton
            color="inherit"
            onClick={() => void refetch()}
            size="small"
          >
            Retry
          </AppButton>
        }
        severity="error"
      >
        {getMutationErrorMessage(error, "Failed to load the Trash")}
      </AppAlert>
    );
  } else if (entries.length === 0) {
    body = <EmptyState message="Trash is empty." />;
  } else {
    listShown = true;
    body = (
      <TrashList
        cardSize={shownCardSize.size}
        entries={entries}
        isDropTarget={dropTarget}
        onDelete={() => askDelete(selectedEntries)}
        onDropped={deselect}
        onLeave={onLeave}
        onOpen={(path) => showDetails([path])}
        onSelectionChange={setSelected}
        onSortChange={(field) => onSortChange(nextTrashSort(sort, field))}
        selectedPaths={selectedPaths}
        sortField={sort.field}
        sortOrder={sort.order}
        viewMode={viewMode}
      />
    );
  }

  const text = confirmText(confirm.kind, confirm.entries);

  return (
    <div
      {...allowContextMenuProps}
      onContextMenu={handleContextMenu}
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        overflow: "hidden",
        position: "relative",
        width: "100%",
      }}
    >
      <div
        className="file-browser-header"
        style={{
          alignItems: "center",
          display: "grid",
          gap: 6,
          gridTemplateColumns: "minmax(0, 1fr) auto",
          minHeight: 64,
          paddingInline: "var(--app-space-8)",
        }}
      >
        <div
          className="file-browser-header__breadcrumbs"
          style={{
            alignItems: "center",
            display: "flex",
            minWidth: 0,
            overflow: "hidden",
          }}
        >
          {breadcrumbs}
        </div>
        <div
          className="file-browser-header__actions"
          style={{ alignItems: "center", display: "flex", justifySelf: "end" }}
        >
          {actions}
        </div>
      </div>
      <div
        {...dropProps}
        ref={listAreaRef}
        style={{
          display: "flex",
          flex: 1,
          flexDirection: "column",
          minHeight: 0,
          paddingBottom: "var(--app-space-8)",
          paddingInline: "var(--app-space-8)",
        }}
      >
        {body}
        {listShown ? null : <LeaveKeys onLeave={onLeave} />}
      </div>

      <AppMenu
        anchorPosition={menuPosition}
        minWidth={200}
        onClose={closeMenu}
        open={menuPosition !== null}
      >
        <AppMenuItem
          disabled={!hasSelection}
          onClick={() => restore(selectedEntries)}
          startAdornment={<Icon icon="mdi:restore" />}
        >
          Restore
        </AppMenuItem>
        <AppMenuItem
          disabled={!hasSelection}
          onClick={() => askRestoreTo(selectedEntries)}
          startAdornment={<Icon icon="mdi:folder-move-outline" />}
        >
          Restore to…
        </AppMenuItem>
        <AppMenuItem
          disabled={!hasSelection}
          onClick={() =>
            showDetails(selectedEntries.map((entry) => entry.path))
          }
          startAdornment={<Icon icon="mdi:information-outline" />}
        >
          Properties
        </AppMenuItem>
        <AppDivider />
        <AppMenuItem
          danger
          disabled={!hasSelection}
          onClick={() => askDelete(selectedEntries)}
          startAdornment={<Icon icon="mdi:delete-forever" />}
        >
          Delete permanently
        </AppMenuItem>
        <AppMenuItem
          danger
          disabled={entries.length === 0}
          onClick={askEmpty}
          startAdornment={<Icon icon="mdi:delete-sweep" />}
        >
          Empty Trash
        </AppMenuItem>
      </AppMenu>

      <TrashDetailsDialog
        entries={detailEntries}
        onClose={() => setDetails((previous) => ({ ...previous, open: false }))}
        onDelete={fromDetails(askDelete)}
        onRestore={fromDetails(restore)}
        onRestoreTo={fromDetails(askRestoreTo)}
        open={details.open}
      />
      <TrashRestoreToDialog
        count={restoreTo.entries.length}
        initialFolder={folderPath}
        key={restoreTo.opened}
        onClose={() =>
          setRestoreTo((previous) => ({ ...previous, open: false }))
        }
        onRestore={confirmRestoreTo}
        open={restoreTo.open}
      />
      <AppConfirmDialog
        confirmText={text.confirm}
        destructive={confirm.kind !== "restore"}
        message={text.message}
        onClose={() => setConfirm((previous) => ({ ...previous, open: false }))}
        onConfirm={confirmAction}
        open={confirm.open}
        title={text.title}
      />
    </div>
  );
};

export default TrashPlace;
