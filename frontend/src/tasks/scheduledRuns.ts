import { isTerminalTaskState, type TaskSnapshot } from "@/api";
import type { BackgroundTask, BackgroundTaskLink } from "@/tasks/types";

// Work a systemd timer runs, reported on the task events stream. These are
// not bridge routes (nothing in the browser starts one), so they do not go
// through taskType(); the bridge answers tasks.watch and tasks.cancel of a
// running one with 409.
export const SCHEDULED_SCRIPT = "scheduled.script";
export const SCHEDULED_BTRFS_SNAPSHOT = "scheduled.btrfs_snapshot";
export const SCHEDULED_BTRFS_RESYNC = "scheduled.btrfs_resync";
export const SCHEDULED_DOCKER_UPDATE = "scheduled.docker_update";
// The distribution's automatic system updates (APT, DNF, Mint).
export const SCHEDULED_SYSTEM_UPDATE = "scheduled.system_update";

const SCHEDULED_RUN_TYPES = new Set<string>([
  SCHEDULED_SCRIPT,
  SCHEDULED_BTRFS_SNAPSHOT,
  SCHEDULED_BTRFS_RESYNC,
  SCHEDULED_DOCKER_UPDATE,
  SCHEDULED_SYSTEM_UPDATE,
]);

export const isScheduledRun = (type: string) => SCHEDULED_RUN_TYPES.has(type);

/** "Label", or "Label: message" once the run reports progress. */
export function scheduledRunLabel(task: TaskSnapshot): string {
  const label = task.metadata?.label || "Scheduled run";
  const message = task.progress?.message;
  return message ? `${label}: ${message}` : label;
}

/** The page that owns the run. */
export function scheduledRunLink(task: TaskSnapshot): BackgroundTaskLink {
  const id = task.metadata?.identity?.[0];
  switch (task.type) {
    case SCHEDULED_SCRIPT:
      return id
        ? { to: "/services/schedules", search: { schedule: id } }
        : { to: "/services/schedules" };
    case SCHEDULED_BTRFS_RESYNC:
      return id ? { to: "/storage", search: { pool: id } } : { to: "/storage" };
    case SCHEDULED_BTRFS_SNAPSHOT:
      return { to: "/storage/subvolumes", search: { view: "snapshots" } };
    case SCHEDULED_DOCKER_UPDATE:
      return { to: "/docker/containers" };
    case SCHEDULED_SYSTEM_UPDATE:
      return { to: "/updates" };
    default:
      return { to: "/" };
  }
}

// What a scheduled run's row shows. Like a generic row it stays below 100%:
// the bell lists a row that leaves at 100% as completed, and a scheduled run
// may still fail after reporting it.
function scheduledRunFields(task: TaskSnapshot) {
  const percentage = task.progress?.percentage;
  return {
    cancelRequested: task.cancel_requested === true,
    indeterminate: percentage === undefined,
    label: scheduledRunLabel(task),
    link: scheduledRunLink(task),
    progress: percentage === undefined ? 0 : Math.min(99, percentage),
  };
}

/**
 * The rows after a scheduled run's event: the run's row added or updated from
 * the event's snapshot, or removed once the run has ended. `rows` itself when
 * nothing changes.
 */
export function applyScheduledRunEvent(
  rows: BackgroundTask[],
  task: TaskSnapshot,
): BackgroundTask[] {
  const known = rows.some((row) => row.id === task.id);
  if (isTerminalTaskState(task.state)) {
    return known ? rows.filter((row) => row.id !== task.id) : rows;
  }
  const fields = scheduledRunFields(task);
  if (known) {
    return rows.map((row) =>
      row.id === task.id ? { ...row, ...fields } : row,
    );
  }
  return [
    ...rows,
    {
      id: task.id,
      taskId: task.id,
      type: "task",
      taskType: task.type,
      // Nothing to abort: the row opens no stream.
      abortController: new AbortController(),
      ...fields,
    },
  ];
}

/** The rows without any scheduled run's; `rows` itself when there is none. */
export function withoutScheduledRuns(rows: BackgroundTask[]): BackgroundTask[] {
  return rows.some((row) => isScheduledRun(row.taskType))
    ? rows.filter((row) => !isScheduledRun(row.taskType))
    : rows;
}
