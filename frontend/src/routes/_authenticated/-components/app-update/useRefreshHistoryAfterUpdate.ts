import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

import { endpointQueryPrefix } from "@/api/query-keys";

/**
 * While an update waits, the bridge answers an empty history. When the wait
 * ends, refetch every `tasks.history` query once so the pages that show it
 * load the real history without the user leaving them.
 */
export function useRefreshHistoryAfterUpdate(updatePending: boolean) {
  const queryClient = useQueryClient();
  const wasPending = useRef(false);

  useEffect(() => {
    if (wasPending.current && !updatePending) {
      void queryClient.invalidateQueries({
        queryKey: endpointQueryPrefix("tasks.history"),
      });
    }
    wasPending.current = updatePending;
  }, [queryClient, updatePending]);
}
