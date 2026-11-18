import { useQueryClient } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";

import { invalidateOperationQueries } from "@/api";

import type { FileBrowserListingQueryOptions } from "../-hooks/fileBrowserListingQueryOptions";
import { useFileBrowserController } from "../-hooks/useFileBrowserController";
import { useFileConflictResolution } from "../-hooks/useFileConflicts";
import { useFileMutations } from "../-hooks/useFileMutations";
import { useFileViewState } from "../-hooks/useFileViewState";
import { type TrashJobs, useTrashJobs } from "../-hooks/useTrashJobs";
import type { TrashDragItem } from "../-internalDrag";
import {
  type TrashSort,
  trashSortFromSearch,
  trashSortSearch,
} from "../-trashEntries";
import FileBrowserBreadcrumbs from "./FileBrowserBreadcrumbs";
import FileBrowserContent from "./FileBrowserContent";
import FileBrowserDialogs from "./FileBrowserDialogs";
import { FileBrowserConflictDialog } from "./FileBrowserPanels";
import NoTrashPrompt from "./NoTrashPrompt";
import TrashPlace from "./TrashPlace";

const fileBrowserRouteApi = getRouteApi("/_authenticated/filebrowser/$");

interface FolderViewProps {
  listingQueryOptions: FileBrowserListingQueryOptions;
  onRestoreItems: (items: TrashDragItem[], destinationDir: string) => void;
}

const FolderView = ({
  listingQueryOptions,
  onRestoreItems,
}: FolderViewProps) => {
  const { contentProps, dialogsProps } = useFileBrowserController(
    listingQueryOptions,
    { onRestoreItems },
  );

  return (
    <>
      <FileBrowserContent
        {...contentProps}
        // The trail subscribes to `_splat` itself, so a directory navigation
        // updates it without re-rendering the header; `onDropItems` is stable.
        breadcrumbs={
          <FileBrowserBreadcrumbs
            onDropItems={contentProps.chrome.onDropItems}
            onRestoreItems={onRestoreItems}
          />
        }
      />
      <FileBrowserDialogs {...dialogsProps} />
    </>
  );
};

// The Trash keeps the folder it was opened from in the URL: its trail leads
// back there, and so do Backspace and Alt+Up.
const TrashView = ({ jobs }: { jobs: TrashJobs }) => {
  const splat = fileBrowserRouteApi.useParams({
    select: (params) => params._splat ?? "",
  });
  const sortField = fileBrowserRouteApi.useSearch({
    select: (search) => search.sort,
  });
  const sortOrder = fileBrowserRouteApi.useSearch({
    select: (search) => search.order,
  });
  const navigate = fileBrowserRouteApi.useNavigate();
  const queryClient = useQueryClient();
  const folderPath = splat ? `/${splat}` : "/";
  const { deleteItems, trashItems } = useFileMutations({
    normalizedPath: folderPath,
  });
  // Cards or the list, as the folder listing shows its items.
  const { actions: viewActions, viewMode } = useFileViewState();

  const leave = () =>
    void navigate({
      params: { _splat: splat },
      search: {},
      to: "/filebrowser/$",
    });
  // A new sort replaces the URL rather than adding a step, so Back still
  // leads to the folder the Trash was opened from.
  const changeSort = (sort: TrashSort) =>
    void navigate({
      replace: true,
      search: (previous) => ({ ...previous, ...trashSortSearch(sort) }),
      to: ".",
    });
  // Items dragged here from a folder, in another tab, go to the Trash. A
  // cancelled move has moved some of them, so the list is read again then.
  const trashDropped = (paths: string[]) => {
    trashItems(paths).catch(() => {
      void invalidateOperationQueries(queryClient, "filebrowser.trash_batch");
    });
  };

  return (
    <>
      <TrashPlace
        breadcrumbs={
          <FileBrowserBreadcrumbs
            onRestoreItems={jobs.restoreInto}
            place={{
              icon: "mdi:trash-can-outline",
              label: "Trash",
              onDropItems: trashDropped,
            }}
          />
        }
        folderPath={folderPath}
        jobs={jobs}
        onLeave={leave}
        onSortChange={changeSort}
        onSwitchView={viewActions.switchView}
        onTrashItems={trashDropped}
        sort={trashSortFromSearch({ order: sortOrder, sort: sortField })}
        viewMode={viewMode}
      />
      <NoTrashPrompt onDelete={deleteItems} />
    </>
  );
};

const FileBrowserPage = () => {
  const fileBrowserListingQueryOptions = fileBrowserRouteApi.useRouteContext({
    select: (context) => context.fileBrowserListingQueryOptions,
  });
  const inTrash = fileBrowserRouteApi.useSearch({
    select: (search) => search.trash === true,
  });
  // Restores belong to the page, not to the folder or the Trash: a restore
  // whose names are taken asks when it ends, which may be after the user
  // went from the Trash to a folder or back, and its question must find a
  // prompt to ask with. Paste keeps its own prompt in the folder view.
  const restoreConflicts = useFileConflictResolution();
  const trashJobs = useTrashJobs({
    resolveCollisions: restoreConflicts.resolveCollisions,
  });

  return (
    <>
      {inTrash ? (
        <TrashView jobs={trashJobs} />
      ) : (
        <FolderView
          listingQueryOptions={fileBrowserListingQueryOptions}
          onRestoreItems={trashJobs.restoreInto}
        />
      )}
      <FileBrowserConflictDialog
        onCancel={restoreConflicts.cancelConflictPrompt}
        onResolve={restoreConflicts.applyConflictDecisions}
        prompt={restoreConflicts.conflictPrompt}
      />
    </>
  );
};

export type { ViewMode } from "../-types";
export default FileBrowserPage;
