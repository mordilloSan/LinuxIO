// src/utils/ReactQueryProvider.tsx
import {
  QueryClient,
  QueryClientProvider,
  QueryCache,
} from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import { AxiosError } from "axios";
import React, { ReactNode } from "react";
import { toast } from "sonner";

function getErrorMessage(error: unknown): string {
  if (error instanceof AxiosError) {
    const data = error.response?.data as any;
    return (
      data?.message ||
      data?.error?.message ||
      error.message ||
      "An unexpected error occurred."
    );
  }
  if (error instanceof Error) return error.message;
  return String(error);
}

function isAuthishStatus(e: unknown) {
  const ax = e as AxiosError | undefined;
  const s = ax?.response?.status;
  return s === 401 || s === 403 || s === 419;
}

function makeQueryClient() {
  return new QueryClient({
    queryCache: new QueryCache({
      onError: (error) => {
        // Suppress toast for auth-related errors; axios interceptor will redirect.
        if (isAuthishStatus(error)) return;
        toast.error(getErrorMessage(error));
      },
    }),
    defaultOptions: {
      queries: {
        // Don’t retry on auth errors; otherwise retry once
        retry: (failureCount, error) =>
          isAuthishStatus(error) ? false : failureCount < 1,
        refetchOnWindowFocus: false,
        staleTime: 2000,
      },
      mutations: {
        onError: (error) => {
          // Mutations often deserve a toast even if auth fails, but you can suppress similarly:
          if (isAuthishStatus(error)) return;
          toast.error(getErrorMessage(error));
        },
      },
    },
  });
}

let browserQueryClient: QueryClient | undefined;

function getQueryClient(): QueryClient {
  if (typeof window === "undefined") return makeQueryClient();
  if (!browserQueryClient) browserQueryClient = makeQueryClient();
  return browserQueryClient;
}

interface ReactQueryProviderProps {
  children: ReactNode;
}

const ReactQueryProvider: React.FC<ReactQueryProviderProps> = ({
  children,
}) => {
  const queryClient = getQueryClient();
  return (
    <QueryClientProvider client={queryClient}>
      {children}
      <ReactQueryDevtools initialIsOpen={false} />
    </QueryClientProvider>
  );
};

export default ReactQueryProvider;
