import { useEffect, useEffectEvent, useMemo, useState } from "react";

import { isTerminalJobState, linuxio, type JobSnapshot } from "@/api";

/**
 * Page-level refresh recovery: finds an already-running job of `type` (e.g.
 * after a page reload) and hands it to `onRecover` so the owning page can
 * re-attach its in-page progress UI. The global recovered-jobs stream keeps
 * ownership of generic indicators and completion invalidation; this hook only
 * restores page-local UI. The lookup runs once per `scanKey` change; pass
 * `null` to disable.
 *
 * `onMiss` fires when the scan finds no matching active job — auto-start
 * surfaces (dialogs that run their job on open) use it to start fresh only
 * after recovery has had its chance, so the two paths cannot race. A failed
 * scan also falls through to `onMiss`: the start then surfaces the real
 * transport error instead of the dialog hanging silently.
 */
export type ActiveJobRecoveryStatus = "pending" | "recovered" | "missed";

export function useActiveJobRecovery({
  type,
  scanKey,
  match,
  onRecover,
  onMiss,
}: {
  type: string;
  scanKey: string | null;
  match: (job: JobSnapshot) => boolean;
  onRecover: (job: JobSnapshot) => void;
  onMiss?: () => void;
}): { status: ActiveJobRecoveryStatus; isScanning: boolean } {
  const [settledScan, setSettledScan] = useState<{
    request: { scanKey: string | null; type: string };
    status: Exclude<ActiveJobRecoveryStatus, "pending">;
  } | null>(null);
  // A fresh identity records each null -> key re-entry as a new scan while
  // keeping ordinary rerenders of an unchanged key to exactly one request.
  const request = useMemo(() => ({ scanKey, type }), [scanKey, type]);
  const matches = useEffectEvent(match);
  const recover = useEffectEvent(onRecover);
  const miss = useEffectEvent(() => onMiss?.());
  const findMatchingJob = useEffectEvent((jobs: JobSnapshot[]) =>
    jobs.find(
      (candidate) =>
        candidate.type === type &&
        !isTerminalJobState(candidate.state) &&
        matches(candidate),
    ),
  );

  useEffect(() => {
    if (request.scanKey === null) return;
    let canceled = false;
    void (async () => {
      let job: JobSnapshot | undefined;
      try {
        const jobs = await linuxio.jobs.list({ status: "active" });
        if (canceled) return;
        job = findMatchingJob(jobs);
      } catch {
        // Best-effort: a missed recovery only loses in-page progress UI.
      }
      if (canceled) return;
      if (job) {
        setSettledScan({ request, status: "recovered" });
        recover(job);
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
