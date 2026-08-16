import { useQuery, useQueryClient } from "@tanstack/react-query";

import { CACHE_TTL_MS, linuxio, useCallMutation } from "@/api";
import { OPERATION_QUERY_INVALIDATIONS } from "@/api/operation-query-invalidations";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

/**
 * Keep the availability snapshot fresh while the Docker area is open. Query
 * ownership deduplicates mount, focus, and interval triggers; the affected
 * Docker lists are invalidated only after the scan completes successfully.
 */
export function useDockerUpdateStatusRefresh() {
  const queryClient = useQueryClient();
  const { isEnabled: dockerUpdatesEnabled } = useCapability(
    "dockerUpdatesAvailable",
  );
  const endpoint = linuxio.docker.check_updates;
  const invalidationKeys = OPERATION_QUERY_INVALIDATIONS[endpoint.route] ?? [];

  return useQuery({
    ...endpoint,
    enabled: dockerUpdatesEnabled,
    staleTime: CACHE_TTL_MS.FIVE_MINUTES,
    refetchInterval: CACHE_TTL_MS.FIVE_MINUTES,
    // Entering the Docker area should always get a current availability scan,
    // even if the query cache still contains a result from a prior visit.
    refetchOnMount: "always",
    refetchOnWindowFocus: true,
    retry: false,
    meta: { silent: true },
    queryFn: async (context) => {
      const result = await endpoint.queryFn(context);
      await Promise.all(
        invalidationKeys.map((queryKey) =>
          queryClient.invalidateQueries({ queryKey }),
        ),
      );
      return result;
    },
  });
}

export function useDockerUpdateCheck() {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const { isEnabled: dockerUpdatesEnabled, reason: dockerUpdatesReason } =
    useCapability("dockerUpdatesAvailable");
  const { mutate: checkUpdates, isPending: isCheckingUpdates } =
    useCallMutation(linuxio.docker.check_updates, {
      success: (result) => {
        const checked = result?.checked ?? 0;
        const errors = result?.errors ?? 0;
        const uncheckable = result?.uncheckable ?? 0;
        const updates = result?.updates ?? 0;
        if (errors > 0) {
          const uncheckableMessage =
            uncheckable > 0 ? `, ${uncheckable} cannot be checked` : "";
          toast.warning(
            `Checked ${checked} container(s), ${errors} check error(s)${uncheckableMessage}, found ${updates} update(s)`,
          );
          return;
        }
        if (uncheckable > 0 && errors === 0) {
          toast.warning(
            `Checked ${checked} container(s), ${uncheckable} cannot be checked, found ${updates} update(s)`,
          );
          return;
        }
        toast.success(
          `Checked ${checked} container(s), found ${updates} update(s)`,
        );
      },
      error: "Failed to check updates",
      toast: DOCKER_TOAST_META,
    });

  return {
    button: (
      <AppActionIconButton
        ariaLabel="Check container updates"
        disabled={!dockerUpdatesEnabled}
        icon="mdi:refresh"
        iconSize={20}
        label={dockerUpdatesEnabled ? "Check Updates" : dockerUpdatesReason}
        loading={isCheckingUpdates}
        onClick={() => checkUpdates()}
      />
    ),
    isCheckingUpdates,
  };
}
