import { useEffect, useEffectEvent } from "react";

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
}) {
  const matches = useEffectEvent(match);
  const recover = useEffectEvent(onRecover);
  const miss = useEffectEvent(() => onMiss?.());

  useEffect(() => {
    if (scanKey === null) return;
    let canceled = false;
    void (async () => {
      let job: JobSnapshot | undefined;
      try {
        const jobs = await linuxio.jobs.list({ status: "active" });
        if (canceled) return;
        job = jobs.find(
          (candidate) =>
            candidate.type === type &&
            !isTerminalJobState(candidate.state) &&
            matches(candidate),
        );
      } catch {
        // Best-effort: a missed recovery only loses in-page progress UI.
      }
      if (canceled) return;
      if (job) {
        recover(job);
      } else {
        miss();
      }
    })();
    return () => {
      canceled = true;
    };
  }, [scanKey, type]);
}
