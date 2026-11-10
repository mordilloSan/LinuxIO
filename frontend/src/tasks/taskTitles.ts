import type { TaskHistoryEntry, TaskSnapshot } from "@/api";
import { isScheduledRun, scheduledRunLabel } from "@/tasks/scheduledRuns";
import * as TaskTypes from "@/tasks/taskTypes";
import { formatDuration } from "@/utils/formatters";
import { baseName } from "@/utils/path";

/**
 * What a file job does to its object, in the words the notifications bell
 * shows ("Copying 12 items", "Moving report.pdf to Trash"). The bell passes
 * the items a running job names or has done, the history an ended job's
 * label. Any other job's label already says what it does and is kept.
 */
export function fileTaskText(
  type: string,
  object: string,
  phase?: string,
): string {
  switch (type) {
    case TaskTypes.TASK_TYPE_FILE_COMPRESS:
      return `Compressing ${object}`;
    case TaskTypes.TASK_TYPE_FILE_EXTRACT:
      return `Extracting ${object}`;
    case TaskTypes.TASK_TYPE_FILE_COPY_BATCH:
      return `Copying ${object}`;
    case TaskTypes.TASK_TYPE_FILE_MOVE_BATCH:
      return `Moving ${object}`;
    case TaskTypes.TASK_TYPE_FILE_DELETE_BATCH:
      return `Deleting ${object}`;
    case TaskTypes.TASK_TYPE_FILE_TRASH_BATCH:
      return `Moving ${object} to Trash`;
    case TaskTypes.TASK_TYPE_FILE_TRASH_RESTORE:
      return `Restoring ${object} from Trash`;
    case TaskTypes.TASK_TYPE_FILE_TRASH_PURGE:
      return `Deleting ${object} from Trash`;
    case TaskTypes.TASK_TYPE_FILE_CHMOD_BATCH:
      return `${phase === "chown" ? "Changing ownership" : "Changing permissions"}: ${object}`;
    default:
      return object;
  }
}

/** An extraction is named by its archive's file name without the extension. */
export function extractionLabelBase(archivePath: string): string {
  const raw = baseName(archivePath) || "archive";
  return raw.replace(/\.(tar\.gz|tgz|zip)$/i, "") || raw;
}

/** A readable name for a job or scheduled run, for lists of ended work. */
export function taskTitle(task: TaskSnapshot): string {
  const label = task.metadata?.label;
  if (label) {
    // The label of an archive job is its path; the bell names the file.
    if (task.type === TaskTypes.TASK_TYPE_FILE_COMPRESS) {
      return fileTaskText(task.type, baseName(label) || label);
    }
    if (task.type === TaskTypes.TASK_TYPE_FILE_EXTRACT) {
      return fileTaskText(task.type, extractionLabelBase(label));
    }
    return fileTaskText(task.type, label);
  }
  if (isScheduledRun(task.type)) return scheduledRunLabel(task);
  // "storage.btrfs_scrub_start" -> "Storage: btrfs scrub start"
  const dot = task.type.indexOf(".");
  if (dot < 0) return task.type;
  const area = task.type.slice(0, dot);
  const action = task.type.slice(dot + 1).replaceAll("_", " ");
  return `${area.charAt(0).toUpperCase()}${area.slice(1)}: ${action}`;
}

/** "Completed", "Failed", "Cancelled", "Interrupted (reboot)". */
export function taskOutcome(entry: TaskHistoryEntry): string {
  switch (entry.task.state) {
    case "completed":
      return "Completed";
    case "failed":
      return "Failed";
    case "canceled":
      return "Cancelled";
    case "interrupted":
      return entry.reason ? `Interrupted (${entry.reason})` : "Interrupted";
    default:
      return entry.task.state;
  }
}

// Exactly as received: the server's times carry nanoseconds that a Date would
// truncate, and the next page of a history would then skip entries.
export const taskEndedAt = (task: TaskSnapshot) =>
  task.finished_at ?? task.started_at ?? task.created_at;

/** How long the run took, as text; null when it has no start or no end. */
export const taskRunTime = (task: TaskSnapshot): string | null =>
  task.started_at && task.finished_at
    ? formatDuration(
        (Date.parse(task.finished_at) - Date.parse(task.started_at)) / 1000,
      )
    : null;
