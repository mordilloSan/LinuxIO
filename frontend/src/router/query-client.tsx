import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { getAppQueryClient } from "./query-client-core";

interface AppQueryClientProviderProps {
  children: ReactNode;
}

export const appQueryClient = getAppQueryClient();

const AppQueryClientProvider = ({ children }: AppQueryClientProviderProps) => {
  return (
    <QueryClientProvider client={appQueryClient}>
      {children}
    </QueryClientProvider>
  );
};

export default AppQueryClientProvider;
