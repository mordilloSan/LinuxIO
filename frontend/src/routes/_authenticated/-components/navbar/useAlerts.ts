import { useQuery, useQueryClient } from "@tanstack/react-query";

import { linuxio, useCallMutation, type AlertList } from "@/api";
import useAuth from "@/hooks/useAuth";

const EMPTY: AlertList = { alerts: [], unseen: 0 };

export function useAlerts() {
  const { privileged } = useAuth();
  const queryClient = useQueryClient();
  const query = useQuery({
    ...linuxio.alerts.list,
    enabled: privileged,
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  });
  const replace = (result: AlertList) => {
    queryClient.setQueryData(linuxio.alerts.list.queryKey, result);
  };
  const markAllSeenMutation = useCallMutation(linuxio.alerts.mark_all_seen, {
    error: "Failed to update alerts",
    invalidates: [],
    success: replace,
  });
  const dismissMutation = useCallMutation(linuxio.alerts.dismiss, {
    error: "Failed to dismiss alert",
    invalidates: [],
    success: replace,
  });
  const data = query.data ?? EMPTY;
  return {
    alerts: data.alerts,
    unseen: data.unseen,
    enabled: privileged,
    markAllSeen: () => markAllSeenMutation.mutate(undefined),
    dismiss: (id: string) => dismissMutation.mutate({ id }),
  };
}
