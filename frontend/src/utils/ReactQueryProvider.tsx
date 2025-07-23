import {
  QueryClient,
  QueryClientProvider,
  QueryCache,
} from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import React, { ReactNode } from "react";
import { toast } from "sonner";

import { getErrorMessage } from "./getErrorMessage";

function makeQueryClient() {
  return new QueryClient({
    queryCache: new QueryCache({
      onError: (error: unknown) => {
        const err = error as Error;
        toast.error(err.message || "An error occurred with the query.");
      },
    }),
    defaultOptions: {
      queries: {
        retry: 1,
        refetchOnWindowFocus: false,
        staleTime: 2000,
      },
      mutations: {
        onError: (error: unknown) => {
          toast.error(getErrorMessage(error));
        },
      },
    },
  });
}

let browserQueryClient: QueryClient | undefined;

function getQueryClient(): QueryClient {
  if (typeof window === "undefined") {
    return makeQueryClient();
  }
  if (!browserQueryClient) {
    browserQueryClient = makeQueryClient();
  }
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
