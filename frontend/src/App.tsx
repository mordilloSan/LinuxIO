import { AuthProvider } from "./contexts/AuthContext";
import { useGlobalContextMenuGuard } from "./hooks/useGlobalContextMenuGuard";
import ApplicationRouterProvider from "./router/provider";
import AppQueryClientProvider from "./router/query-client";

function App() {
  // Disable right-click globally except where explicitly allowed
  useGlobalContextMenuGuard();

  return (
    <AuthProvider>
      <AppQueryClientProvider>
        <ApplicationRouterProvider />
      </AppQueryClientProvider>
    </AuthProvider>
  );
}

export default App;
