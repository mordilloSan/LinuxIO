import { onlineManager, QueryCache, QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { CACHE_TTL_MS } from "./call-react-query";
import { isConnectionLossError } from "./errors";
import { isRequestAvailable, subscribeRequestAvailability } from "./linuxio";

onlineManager.setEventListener((setOnline) => {
  const updateOnlineState = () => {
    const browserOnline = typeof navigator === "undefined" || navigator.onLine;
    setOnline(browserOnline && isRequestAvailable());
  };

  updateOnlineState();
  const unsubscribeTransport = subscribeRequestAvailability(updateOnlineState);
  globalThis.addEventListener?.("online", updateOnlineState);
  globalThis.addEventListener?.("offline", updateOnlineState);

  return () => {
    unsubscribeTransport();
    globalThis.removeEventListener?.("online", updateOnlineState);
    globalThis.removeEventListener?.("offline", updateOnlineState);
  };
});

function getErrorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  return String(error);
}

function retryQuery(failureCount: number, error: unknown): boolean {
  // Endpoints with an explicit connection-loss policy already retry once inside
  // the transport. Starting a second Query attempt would multiply that policy.
  if (isConnectionLossError(error)) {
    return false;
  }
  return failureCount < 1;
}

/** Build an isolated QueryClient; tests use one per case. */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    queryCache: new QueryCache({
      onError: (error, query) => {
        if (query.meta?.silent) return;
        toast.error(getErrorMessage(error));
      },
    }),
    defaultOptions: {
      queries: {
        retry: retryQuery,
        refetchOnWindowFocus: false,
        staleTime: CACHE_TTL_MS.TWO_SECONDS,
      },
      mutations: {
        onError: (error) => {
          toast.error(getErrorMessage(error));
        },
      },
    },
  });
}

/** The application's cache, shared by providers and route loaders. */
export const appQueryClient = createQueryClient();
