// Types and helpers for structured `docker compose --progress=json` events.
// The backend (compose_sdk.go / background_operations.go) parses Docker's JSON
// progress stream and forwards each generated ComposeProgress event; this
// module turns that stream into per-layer task state for DockerComposeProgress.

import type { ComposeProgress } from "@/api";

export type { ComposeProgress } from "@/api";

// ComposeTask is a ComposeProgress enriched with a monotonic best-known percent
// (`pct`). Docker resets current/total between the download and extract phases,
// so pct gives a stable, non-regressing value for the bar and the aggregate.
export interface ComposeTask extends ComposeProgress {
  pct: number; // 0-100
}

// mergeTask upserts a progress event into the task map keyed by id. This keeps
// state at O(layers) instead of O(events), and pins finished layers to 100%.
export function mergeTask(
  prev: Map<string, ComposeTask>,
  p: ComposeProgress,
): Map<string, ComposeTask> {
  const next = new Map(prev);
  const existing = next.get(p.id);

  let pct = existing?.pct ?? 0;
  if (p.status === "Done") {
    pct = 100;
  } else if (typeof p.percent === "number" && p.percent > pct) {
    pct = p.percent;
  }

  // Preserve the last known positive total (extract events report total 0).
  const total = p.total && p.total > 0 ? p.total : existing?.total;

  next.set(p.id, { ...existing, ...p, total, pct });
  return next;
}

// isLayer reports whether a task is a pull layer (has a parent image) rather
// than a group row (Image/Container/Network/Volume).
export function isLayer(t: ComposeTask): boolean {
  return Boolean(t.parent_id);
}

// aggregatePercent is the mean completion across pull layers, or null when
// there are no layers (e.g. down/stop, or images already cached) — in which
// case the UI shows plain status rows with no overall bar.
export function aggregatePercent(tasks: ComposeTask[]): number | null {
  const layers = tasks.filter(isLayer);
  if (layers.length === 0) return null;
  const sum = layers.reduce((acc, t) => acc + t.pct, 0);
  return Math.round(sum / layers.length);
}

// prettyId strips Docker's resource prefix for display ("Image alpine" -> "alpine").
export function prettyId(id: string): string {
  return id.replace(/^(Image|Container|Network|Volume)\s+/, "");
}

// shortId trims a layer hash for compact display.
export function shortId(id: string): string {
  return id.length > 12 ? id.slice(0, 12) : id;
}
