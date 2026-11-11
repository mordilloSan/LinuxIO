import { useQuery } from "@tanstack/react-query";

import { linuxio, useStreamMux } from "@/api";
import type { TaskSnapshot } from "@/api";

/** How often to ask again while an update waits for running jobs. */
const UPDATE_PENDING_POLL_MS = 15_000;

const NOTHING_WAITING = {
  updatePending: false,
  running: 0,
  jobs: [] as TaskSnapshot[],
};

/**
 * Whether a LinuxIO update waits for running jobs of an older job service,
 * how many, and which. An error with no earlier answer (a job service built
 * before this route answers "route not found") counts as nothing waiting; a
 * failed poll after an answer keeps that answer until the next poll.
 */
export function useJobHostStatus() {
  const { isOpen } = useStreamMux();
  const query = useQuery({
    ...linuxio.tasks.host,
    enabled: isOpen,
    refetchInterval: (current) =>
      current.state.data?.update_pending ? UPDATE_PENDING_POLL_MS : false,
    retry: false,
    meta: { silent: true },
  });

  if (!query.data) return NOTHING_WAITING;
  return {
    updatePending: query.data.update_pending,
    running: query.data.running,
    jobs: query.data.jobs ?? [],
  };
}
