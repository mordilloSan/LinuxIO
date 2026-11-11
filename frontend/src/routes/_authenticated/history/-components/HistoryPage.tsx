import { useInfiniteQuery } from "@tanstack/react-query";
import { type CSSProperties, useEffect, useRef, useState } from "react";

import { call, linuxio, type TaskHistoryEntry } from "@/api";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import SavedOutput from "@/components/tasks/SavedOutput";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppRouterLinkButton from "@/components/ui/AppRouterLinkButton";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useConfig } from "@/hooks/useConfig";
import { useJobHostStatus } from "@/routes/_authenticated/-components/app-update/useJobHostStatus";
import { isScheduledRun, scheduledRunLink } from "@/tasks/scheduledRuns";
import {
  taskEndedAt,
  taskOutcome,
  taskRunTime,
  taskTitle,
} from "@/tasks/taskTitles";
import { formatDate } from "@/utils/formatters";
import { logSourceHue } from "@/utils/logHue";

import "@/components/tables/log-row.css";

// The log tables' level marker, reused for how a run ended: green for
// completed, a red rail for failed, amber for one that was stopped.
const outcomeLevel = (state: string) =>
  state === "completed" ? "info" : state === "failed" ? "error" : "warn";

const OUTCOME_FILTERS = [
  { value: "completed", label: "Completed" },
  { value: "failed", label: "Failed" },
  { value: "canceled", label: "Cancelled" },
  { value: "interrupted", label: "Interrupted" },
];

const columns: AppVirtualTableColumnDef<TaskHistoryEntry>[] = [
  {
    id: "ended",
    header: "Ended",
    enableSorting: false,
    cell: ({ row }) => (
      <AppTypography
        className="log-time"
        color="text.secondary"
        noWrap
        title={taskEndedAt(row.original.task)}
        variant="body2"
      >
        {formatDate(taskEndedAt(row.original.task))}
      </AppTypography>
    ),
    // Wider than the log tables' clock time: this one carries the date.
    meta: { width: "224px" },
  },
  {
    id: "level",
    header: "",
    enableSorting: false,
    cell: ({ row }) => (
      <span
        className="log-level"
        data-level={outcomeLevel(row.original.task.state)}
        title={taskOutcome(row.original)}
      />
    ),
    meta: { cellClassName: "log-level-cell", width: "12px" },
  },
  {
    id: "title",
    header: "Job",
    enableSorting: false,
    cell: ({ row }) => taskTitle(row.original.task),
    meta: { align: "left" },
  },
  {
    id: "outcome",
    header: "Outcome",
    enableSorting: false,
    cell: ({ row }) => taskOutcome(row.original),
    meta: { align: "left", width: "minmax(110px, 220px)" },
  },
  {
    id: "owner",
    header: "Started by",
    enableSorting: false,
    cell: ({ row }) => {
      const username = row.original.task.owner?.username;
      return username ? (
        <Chip
          className="log-source"
          label={username}
          size="xsmall"
          style={{ "--log-hue": logSourceHue(username) } as CSSProperties}
          variant="soft"
        />
      ) : null;
    },
    meta: { hideBelow: "md", width: "minmax(96px, 160px)" },
  },
  {
    id: "duration",
    header: "Ran for",
    enableSorting: false,
    cell: ({ row }) => (
      <AppTypography
        className="log-time"
        color="text.secondary"
        noWrap
        variant="body2"
      >
        {taskRunTime(row.original.task) ?? ""}
      </AppTypography>
    ),
    meta: { hideBelow: "sm", width: "96px" },
  },
];

interface Props {
  selected?: string;
  onSelectedChange: (id: string | null) => void;
}

export default function HistoryPage({ selected, onSelectedChange }: Props) {
  const { updatePending } = useJobHostStatus();
  const { updateConfig } = useConfig();
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
  const [outcome, setOutcome] = useState("");
  const entries = query.data?.pages.flatMap((page) => page.entries);
  const visible = entries?.filter(
    (entry) => !outcome || entry.task.state === outcome,
  );
  const selectedEntry = entries?.find((entry) => entry.task.id === selected);
  // While an update waits the job service answers with no entries at all.
  const unavailable = updatePending && entries?.length === 0;

  // Seeing the history is looking at it: the sign-in notice then counts only
  // the jobs that end after this visit. Only a loaded history with entries in
  // it, and no update hiding it, has been seen; that is stored once a visit.
  const historyShown =
    query.isSuccess && !updatePending && (entries?.length ?? 0) > 0;
  const storedSeenAtRef = useRef(false);
  useEffect(() => {
    if (!historyShown || storedSeenAtRef.current) return;
    storedSeenAtRef.current = true;
    updateConfig({ dismissals: { historySeenAt: new Date().toISOString() } });
  }, [historyShown, updateConfig]);

  return (
    // Fills the route's content area like the logs page does: a percentage
    // height resolves against MainLayout's `data-app-route-content` box, so
    // the table below has height to fill.
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-8)",
        height: "100%",
        minHeight: 0,
      }}
    >
      {/* The Docker logs toolbar: a filter, then how much is on screen. */}
      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          gap: "var(--app-space-8)",
        }}
      >
        <AppSelect
          label="Outcome"
          onChange={(event) => setOutcome(event.target.value)}
          style={{ minWidth: 180 }}
          value={outcome}
        >
          <option value="">All outcomes</option>
          {OUTCOME_FILTERS.map(({ value, label }) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </AppSelect>
        {visible && (
          <AppTypography fontWeight={700}>{visible.length} shown</AppTypography>
        )}
      </div>
      {query.isError && (
        <AppAlert severity="error">
          Unable to load the history: {query.error.message}
        </AppAlert>
      )}
      {query.isPending && <AppLinearProgress aria-label="Loading history" />}
      {entries && visible && (
        <>
          <div style={{ flex: "1 1 0", minHeight: 0 }}>
            <AppVirtualTable
              ariaLabel="Job history"
              columns={columns}
              data={visible}
              density="dense"
              emptyMessage={
                entries.length > 0
                  ? "No matching jobs."
                  : unavailable
                    ? "History is unavailable until the LinuxIO update finishes."
                    : "Nothing has ended yet."
              }
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
      {/* An alert links to its job, which may have left the first page. */}
      {selected && entries && !selectedEntry && !unavailable && (
        <AppAlert severity="info">
          {query.hasNextPage
            ? "This job is not among the loaded entries. Load older to look for it."
            : "This job is not among the loaded entries."}
        </AppAlert>
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
        gap: "var(--app-space-8)",
        maxHeight: "50%",
        overflowY: "auto",
        borderTop: "1px solid var(--app-palette-divider)",
        paddingTop: "var(--app-space-8)",
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
        <span className="log-level" data-level={outcomeLevel(task.state)} />
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
