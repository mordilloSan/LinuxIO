import {
  onlineManager,
  QueryCache,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import type { ReactNode } from "react";
import { toast } from "sonner";

import {
  CACHE_TTL_MS,
  isRequestAvailable,
  LinuxIOError,
  subscribeRequestAvailability,
} from "@/api";

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
  // Read endpoints already retry a closed connection once inside the transport.
  // Starting a second Query attempt would multiply the same recovery policy.
  if (error instanceof LinuxIOError && error.code === "connection_closed") {
    return false;
  }
  return failureCount < 1;
}

/**
 * Build an isolated QueryClient.
 *
 * Server renders and tests must use a fresh client per invocation; browser code
 * should instead use getAppQueryClient() so providers and route loaders share
 * one cache.
 */
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

let browserQueryClient: QueryClient | undefined;

export function getAppQueryClient(): QueryClient {
  if (typeof window === "undefined") return createQueryClient();
  if (!browserQueryClient) browserQueryClient = createQueryClient();
  return browserQueryClient;
}

interface AppQueryClientProviderProps {
  children: ReactNode;
}

const AppQueryClientProvider = ({ children }: AppQueryClientProviderProps) => {
  const queryClient = getAppQueryClient();
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
};

export default AppQueryClientProvider;
