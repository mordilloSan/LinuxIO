import type { FileBatchItemFailure } from "@/api";

/** Per-item failures of a batch task result; empty for results without any. */
export function batchFailures(result: unknown): FileBatchItemFailure[] {
  const failed = (result as { failed?: unknown } | null | undefined)?.failed;
  return Array.isArray(failed) ? (failed as FileBatchItemFailure[]) : [];
}

function baseName(path: string): string {
  return path.replace(/\/+$/, "").split("/").pop() || path;
}

/** One `name: error` line per failure, truncated to `max` plus "and N more". */
export function describeBatchFailures(
  failed: FileBatchItemFailure[],
  max = 5,
): string {
  const lines = failed
    .slice(0, max)
    .map((item) => `${baseName(item.path)}: ${item.error}`);
  if (failed.length > max) lines.push(`and ${failed.length - max} more`);
  return lines.join("\n");
}
