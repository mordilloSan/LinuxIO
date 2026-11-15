import type { FileBatchItemFailure } from "@/api";
import type { FileErrorOpts } from "@/utils/linkRefusal";

/** Per-item failures of a batch task result; empty for results without any. */
export function batchFailures(result: unknown): FileBatchItemFailure[] {
  const failed = (result as { failed?: unknown } | null | undefined)?.failed;
  return Array.isArray(failed) ? (failed as FileBatchItemFailure[]) : [];
}

function baseName(path: string): string {
  return path.replace(/\/+$/, "").split("/").pop() || path;
}

function failureLine(item: FileBatchItemFailure): string {
  return `${baseName(item.path)}: ${item.error}`;
}

/** One `name: error` line per failure, truncated to `max` plus "and N more". */
export function describeBatchFailures(
  failed: FileBatchItemFailure[],
  max = 5,
): string {
  const lines = failed.slice(0, max).map(failureLine);
  if (failed.length > max) lines.push(`and ${failed.length - max} more`);
  return lines.join("\n");
}

/**
 * Toast options for a batch's failures: the shortened list as the description,
 * and every failure's line for `toastFileError` to search for a link refusal.
 */
export function batchFailureOpts(
  failed: FileBatchItemFailure[],
): FileErrorOpts {
  return {
    description: describeBatchFailures(failed),
    failures: failed.map(failureLine),
  };
}
