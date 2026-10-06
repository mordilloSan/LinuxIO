export interface DockerLogEntry {
  /** Short container id. */
  id: string;
  line: string;
  name: string;
  /** Arrival order; the row id and the tie-breaker for equal timestamps. */
  seq: number;
  stderr: boolean;
  /** Docker RFC3339Nano timestamp, or "" when the line had none. */
  ts: string;
}

export const DOCKER_LOG_BUFFER_LIMIT = 5000;

// Hour, minute and second spelled out: { hour12 } alone formats a date.
const TIME_FORMATTER = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

/** One Channel frame holds one or more NDJSON records; malformed lines are skipped. */
export function parseDockerLogFrame(
  text: string,
  firstSeq: number,
): DockerLogEntry[] {
  const entries: DockerLogEntry[] = [];
  for (const raw of text.split("\n")) {
    if (!raw) continue;
    let record: unknown;
    try {
      record = JSON.parse(raw);
    } catch {
      continue;
    }
    if (typeof record !== "object" || record === null) continue;
    const { id, name, ts, line, stderr } = record as Record<string, unknown>;
    if (
      typeof id !== "string" ||
      typeof name !== "string" ||
      typeof line !== "string"
    ) {
      continue;
    }
    entries.push({
      id,
      line,
      name,
      seq: firstSeq + entries.length,
      stderr: stderr === true,
      ts: typeof ts === "string" ? ts : "",
    });
  }
  return entries;
}

// Docker timestamps are UTC with nine fractional digits, so they order as strings.
function compareDockerLogEntries(a: DockerLogEntry, b: DockerLogEntry): number {
  if (a.ts && b.ts && a.ts !== b.ts) return a.ts < b.ts ? -1 : 1;
  return a.seq - b.seq;
}

/**
 * Appends a flushed batch in timestamp order and drops the oldest entries past
 * `limit`. The backlog of several containers arrives interleaved, so a batch
 * that starts before the newest buffered line re-sorts the whole buffer; live
 * batches just append.
 */
export function mergeDockerLogEntries(
  current: DockerLogEntry[],
  batch: DockerLogEntry[],
  limit = DOCKER_LOG_BUFFER_LIMIT,
): DockerLogEntry[] {
  if (batch.length === 0) return current;
  const sorted = batch.toSorted(compareDockerLogEntries);
  const newest = current.at(-1);
  // ponytail: O(n log n) resort on out-of-order batches; a merge pass if the
  // 5000-entry cap ever grows.
  const next =
    newest && compareDockerLogEntries(sorted[0], newest) < 0
      ? [...current, ...sorted].sort(compareDockerLogEntries)
      : current.concat(sorted);
  return next.length > limit ? next.slice(next.length - limit) : next;
}

/** Deterministic hue for a container name so its badge colour is stable across sessions. */
export function dockerLogHue(name: string): number {
  let hash = 0;
  for (const char of name) {
    hash = (hash * 31 + (char.codePointAt(0) ?? 0)) >>> 0;
  }
  return hash % 360;
}

export function formatDockerLogTime(ts: string): string {
  if (!ts) return "";
  const date = new Date(ts);
  return Number.isNaN(date.getTime()) ? ts : TIME_FORMATTER.format(date);
}

/**
 * Stages a parsed frame for the next flush, keeping only the newest `limit`
 * entries so a paused animation frame (background tab) cannot grow the queue
 * without bound.
 */
export function stageDockerLogEntries(
  pending: DockerLogEntry[],
  batch: DockerLogEntry[],
  limit = DOCKER_LOG_BUFFER_LIMIT,
): void {
  for (const entry of batch) pending.push(entry);
  if (pending.length > limit) pending.splice(0, pending.length - limit);
}
