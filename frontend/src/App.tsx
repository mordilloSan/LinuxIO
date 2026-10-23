import { QueryClientProvider } from "@tanstack/react-query";

import { appQueryClient } from "./api/query-client";
import { AuthProvider } from "./contexts/AuthProvider";
import { useGlobalContextMenuGuard } from "./hooks/useGlobalContextMenuGuard";
import ApplicationRouterProvider from "./router/provider";

function App() {
  // Disable right-click globally except where explicitly allowed
  useGlobalContextMenuGuard();

  return (
    <QueryClientProvider client={appQueryClient}>
      <AuthProvider>
        <ApplicationRouterProvider />
      </AuthProvider>
    </QueryClientProvider>
  );
}

export default App;
