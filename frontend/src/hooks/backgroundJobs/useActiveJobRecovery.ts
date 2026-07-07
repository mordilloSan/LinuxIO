import { useEffect, useEffectEvent } from "react";

import { isTerminalJobState, linuxio, type JobSnapshot } from "@/api";

/**
 * Page-level refresh recovery: finds an already-running job of `type` (e.g.
 * after a page reload) and hands it to `onRecover` so the owning page can
 * re-attach its in-page progress UI. The global recovered-jobs stream keeps
 * ownership of generic indicators and completion invalidation; this hook only
 * restores page-local UI. The lookup runs once per `scanKey` change; pass
 * `null` to disable.
 */
export function useActiveJobRecovery({
  type,
  scanKey,
  match,
  onRecover,
}: {
  type: string;
  scanKey: string | null;
  match: (job: JobSnapshot) => boolean;
  onRecover: (job: JobSnapshot) => void;
}) {
  const matches = useEffectEvent(match);
  const recover = useEffectEvent(onRecover);

  useEffect(() => {
    if (scanKey === null) return;
    let canceled = false;
    void (async () => {
      try {
        const jobs = await linuxio.jobs.list({ status: "active" });
        if (canceled) return;
        const job = jobs.find(
          (candidate) =>
            candidate.type === type &&
            !isTerminalJobState(candidate.state) &&
            matches(candidate),
        );
        if (job) recover(job);
      } catch {
        // Best-effort: a missed recovery only loses in-page progress UI.
      }
    })();
    return () => {
      canceled = true;
    };
  }, [scanKey, type]);
}
