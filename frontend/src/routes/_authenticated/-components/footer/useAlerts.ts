import { useQuery, useQueryClient } from "@tanstack/react-query";

import { linuxio, type AlertList } from "@/api";
import useAuth from "@/hooks/useAuth";

const EMPTY: AlertList = { alerts: [], unseen: 0 };

/**
 * Marks every alert seen and keeps the list the bridge answers with: the
 * bell's tray on open, and the sign-in notice's Show.
 */
export function useMarkAlertsSeen(): () => void {
  const queryClient = useQueryClient();
  const { mutate } = linuxio.alerts.mark_all_seen.useMutation({
    error: "Failed to update alerts",
    invalidates: [],
    success: (result) => {
      queryClient.setQueryData(linuxio.alerts.list.queryKey, result);
    },
  });
  return () => mutate(undefined);
}

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
  const markAllSeen = useMarkAlertsSeen();
  const dismissMutation = linuxio.alerts.dismiss.useMutation({
    error: "Failed to dismiss alert",
    invalidates: [],
    success: replace,
  });
  const data = query.data ?? EMPTY;
  return {
    alerts: data.alerts,
    unseen: data.unseen,
    enabled: privileged,
    markAllSeen,
    dismiss: (id: string) => dismissMutation.mutate({ id }),
  };
}
