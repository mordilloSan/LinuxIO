import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { type AppConfig, linuxio, type TaskHistoryEntry } from "@/api";
import { bridgeConfigQueryKey } from "@/api/config-query";
import useAuth from "@/hooks/useAuth";
import { useConfig, useConfigUserId } from "@/hooks/useConfig";
import { taskEndedAt } from "@/tasks/taskTitles";
import { hasFreshSignIn, takeFreshSignIn } from "@/utils/freshSignIn";

import { openNotificationsPanel } from "./footer/notificationsPanel";
import { useMarkAlertsSeen } from "./footer/useAlerts";

// Long enough to read the notice and reach its Show button; the app's default
// suits toasts that have nothing to press.
const AWAY_NOTICE_DURATION_MS = 10_000;

/** Jobs that failed or were interrupted after the given time. */
const unfinishedSince = (entries: TaskHistoryEntry[], since: number) =>
  entries.filter(
    ({ task }) =>
      (task.state === "failed" || task.state === "interrupted") &&
      Date.parse(taskEndedAt(task)) > since,
  ).length;

/**
 * Says once, right after a sign-in, what happened while the user was away. An
 * admin hears of the notifications they have not seen; another user, who
 * cannot read alerts, of their own jobs that did not finish since they last
 * opened the history. A page reload in the same session says nothing.
 */
export function useAwayNotice(): void {
  const { privileged } = useAuth();
  const { updateConfig } = useConfig();
  const navigate = useNavigate();
  const userId = useConfigUserId();
  // Only looked at here; the Effect below takes the mark, once, when the
  // first load has settled either way.
  const [freshSignIn] = useState(hasFreshSignIn);
  const alerts = useQuery({
    ...linuxio.alerts.list,
    enabled: freshSignIn && privileged,
  });
  // The first page only, asked once: it is not needed after the notice. The
  // history page reports its own failures.
  const history = useQuery({
    ...linuxio.tasks.history({}),
    enabled: freshSignIn && !privileged,
    meta: { silent: true },
    staleTime: Number.POSITIVE_INFINITY,
  });
  const { data: historySeenAt } = useQuery({
    ...linuxio.config.get,
    queryKey: bridgeConfigQueryKey(userId),
    enabled: false,
    select: (config: AppConfig) => config.dismissals?.historySeenAt,
  });
  // Showing the alerts is looking at them, as opening the bell is.
  const markAlertsSeen = useMarkAlertsSeen();
  const alertsPending = alerts.isPending;
  const unseen = alerts.data?.unseen;
  const historyPending = history.isPending;
  const entries = history.data?.entries;

  useEffect(() => {
    if (!freshSignIn) return;
    if (privileged) {
      // A failed load takes the mark too: a later answer is no longer news.
      if (alertsPending || !takeFreshSignIn() || !unseen) return;
      toast.info(
        `While you were away: ${unseen} new notification${unseen === 1 ? "" : "s"}`,
        {
          action: {
            label: "Show",
            onClick: () => {
              markAlertsSeen();
              openNotificationsPanel();
            },
          },
          duration: AWAY_NOTICE_DURATION_MS,
        },
      );
      return;
    }
    if (historyPending || !takeFreshSignIn() || entries === undefined) return;
    const seenAt = historySeenAt ? Date.parse(historySeenAt) : Number.NaN;
    if (Number.isNaN(seenAt)) {
      // The first sign-in with no stored time sets the baseline, so the jobs
      // that failed before it are not news.
      updateConfig({ dismissals: { historySeenAt: new Date().toISOString() } });
      return;
    }
    const unfinished = unfinishedSince(entries, seenAt);
    if (unfinished === 0) return;
    toast.warning(
      `While you were away: ${unfinished} of your jobs did not finish`,
      {
        action: {
          label: "Show",
          onClick: () => void navigate({ to: "/history" }),
        },
        duration: AWAY_NOTICE_DURATION_MS,
      },
    );
  }, [
    freshSignIn,
    privileged,
    alertsPending,
    unseen,
    historyPending,
    entries,
    historySeenAt,
    updateConfig,
    navigate,
    markAlertsSeen,
  ]);
}
