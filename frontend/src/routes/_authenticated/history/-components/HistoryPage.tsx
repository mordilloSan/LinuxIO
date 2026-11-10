import { useInfiniteQuery } from "@tanstack/react-query";

import { call, linuxio, type TaskHistoryEntry } from "@/api";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import SavedOutput from "@/components/tasks/SavedOutput";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppRouterLinkButton from "@/components/ui/AppRouterLinkButton";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { isScheduledRun, scheduledRunLink } from "@/tasks/scheduledRuns";
import {
  taskEndedAt,
  taskOutcome,
  taskRunTime,
  taskTitle,
} from "@/tasks/taskTitles";
import { formatDate } from "@/utils/formatters";

const columns: AppVirtualTableColumnDef<TaskHistoryEntry>[] = [
  {
    id: "title",
    header: "Job",
    cell: ({ row }) => taskTitle(row.original.task),
  },
  {
    id: "outcome",
    header: "Outcome",
    cell: ({ row }) => taskOutcome(row.original),
  },
  {
    id: "owner",
    header: "Started by",
    cell: ({ row }) => row.original.task.owner?.username ?? "",
  },
  {
    id: "ended",
    header: "Ended",
    cell: ({ row }) => formatDate(taskEndedAt(row.original.task)),
  },
  {
    id: "duration",
    header: "Ran for",
    cell: ({ row }) => taskRunTime(row.original.task) ?? "",
  },
];

interface Props {
  selected?: string;
  onSelectedChange: (id: string | null) => void;
}

export default function HistoryPage({ selected, onSelectedChange }: Props) {
  const query = useInfiniteQuery({
    queryKey: [...linuxio.tasks.history({}).queryKey, "pages"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      call("tasks.history", pageParam ? { before: pageParam } : {}, { signal }),
    getNextPageParam: (last) => {
      const oldest = last.entries.at(-1);
      return last.more && oldest ? taskEndedAt(oldest.task) : undefined;
    },
  });
  const entries = query.data?.pages.flatMap((page) => page.entries);
  const selectedEntry = entries?.find((entry) => entry.task.id === selected);

  return (
    // Fills the route's content area like the logs page does: a percentage
    // height resolves against MainLayout's `data-app-route-content` box, so
    // the table below has height to fill.
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-12)",
        height: "100%",
        minHeight: 0,
      }}
    >
      <AppTypography variant="h5">History</AppTypography>
      <AppTypography color="text.secondary">
        Jobs and scheduled runs that have ended, newest first.
      </AppTypography>
      {query.isError && (
        <AppAlert severity="error">
          Unable to load the history: {query.error.message}
        </AppAlert>
      )}
      {query.isPending && <AppLinearProgress aria-label="Loading history" />}
      {entries && (
        <>
          <div style={{ flex: "1 1 0", minHeight: 0 }}>
            <AppVirtualTable
              ariaLabel="Job history"
              columns={columns}
              data={entries}
              emptyMessage="Nothing has ended yet."
              fillAvailable
              getRowId={(entry) => entry.task.id}
              onRowClick={({ original }) => onSelectedChange(original.task.id)}
              selectedRowId={selected}
            />
          </div>
          {query.hasNextPage && (
            <div>
              <AppButton
                disabled={query.isFetchingNextPage}
                onClick={() => query.fetchNextPage()}
              >
                Load older
              </AppButton>
            </div>
          )}
        </>
      )}
      {selectedEntry && (
        <Details entry={selectedEntry} onClose={() => onSelectedChange(null)} />
      )}
    </div>
  );
}

function Details({
  entry,
  onClose,
}: {
  entry: TaskHistoryEntry;
  onClose: () => void;
}) {
  const { privileged } = useAuth();
  const { task } = entry;
  const link = isScheduledRun(task.type) ? scheduledRunLink(task) : null;

  return (
    // At most half the page, scrolling inside, so the table keeps the rest.
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-12)",
        maxHeight: "50%",
        overflowY: "auto",
      }}
    >
      <div
        style={{
          display: "flex",
          gap: 8,
          alignItems: "center",
          flexWrap: "wrap",
        }}
      >
        <AppTypography variant="h6">{taskTitle(task)}</AppTypography>
        <AppTypography color="text.secondary">
          {taskOutcome(entry)}
        </AppTypography>
        {link && (
          <AppRouterLinkButton search={link.search} size="small" to={link.to}>
            Open
          </AppRouterLinkButton>
        )}
        <AppButton onClick={onClose} size="small">
          Close
        </AppButton>
      </div>
      {task.error?.message && (
        <AppAlert severity="error">{task.error.message}</AppAlert>
      )}
      {entry.output && privileged && (
        // Keyed on the run so another one starts from its own end.
        <SavedOutput key={task.id} taskId={task.id} />
      )}
    </div>
  );
}
