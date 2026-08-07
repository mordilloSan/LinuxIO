import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { getAppQueryClient } from "./query-client-core";

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
