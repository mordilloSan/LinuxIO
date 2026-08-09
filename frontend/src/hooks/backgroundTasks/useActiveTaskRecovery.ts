import { useEffect, useEffectEvent, useMemo, useState } from "react";

import { call, isTerminalTaskState, type TaskSnapshot } from "@/api";

/**
 * Page-level refresh recovery: finds an already-running task of `type` (e.g.
 * after a page reload) and hands it to `onRecover` so the owning page can
 * resume its in-page progress UI. The global recovered-tasks stream keeps
 * ownership of generic indicators and completion invalidation; this hook only
 * restores page-local UI. The lookup runs once per `scanKey` change; pass
 * `null` to disable.
 *
 * `onMiss` fires when the scan finds no matching active task — auto-start
 * surfaces (dialogs that run their task on open) use it to start fresh only
 * after recovery has had its chance, so the two paths cannot race. A failed
 * scan also falls through to `onMiss`: the start then surfaces the real
 * transport error instead of the dialog hanging silently.
 */
export type ActiveTaskRecoveryStatus = "pending" | "recovered" | "missed";

export function useActiveTaskRecovery({
  type,
  scanKey,
  match,
  onRecover,
  onMiss,
}: {
  type: string;
  scanKey: string | null;
  match: (task: TaskSnapshot) => boolean;
  onRecover: (task: TaskSnapshot) => void;
  onMiss?: () => void;
}): { status: ActiveTaskRecoveryStatus; isScanning: boolean } {
  const [settledScan, setSettledScan] = useState<{
    request: { scanKey: string | null; type: string };
    status: Exclude<ActiveTaskRecoveryStatus, "pending">;
  } | null>(null);
  // A fresh identity records each null -> key re-entry as a new scan while
  // keeping ordinary rerenders of an unchanged key to exactly one request.
  const request = useMemo(() => ({ scanKey, type }), [scanKey, type]);
  const matches = useEffectEvent(match);
  const recover = useEffectEvent(onRecover);
  const miss = useEffectEvent(() => onMiss?.());
  const findMatchingTask = useEffectEvent((tasks: TaskSnapshot[]) =>
    tasks.find(
      (candidate) =>
        candidate.type === type &&
        !isTerminalTaskState(candidate.state) &&
        matches(candidate),
    ),
  );

  useEffect(() => {
    if (request.scanKey === null) return;
    let canceled = false;
    void (async () => {
      let task: TaskSnapshot | undefined;
      try {
        const tasks = await call("tasks.list", { status: "active" });
        if (canceled) return;
        task = findMatchingTask(tasks);
      } catch {
        // Best-effort: a missed recovery only loses in-page progress UI.
      }
      if (canceled) return;
      if (task) {
        setSettledScan({ request, status: "recovered" });
        recover(task);
      } else {
        setSettledScan({ request, status: "missed" });
        miss();
      }
    })();
    return () => {
      canceled = true;
    };
  }, [request]);

  const status =
    request.scanKey === null
      ? "missed"
      : settledScan?.request === request
        ? settledScan.status
        : "pending";
  return { status, isScanning: status === "pending" };
}
