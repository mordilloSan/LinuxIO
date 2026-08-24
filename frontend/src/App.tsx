import { AuthProvider } from "./contexts/AuthContext";
import { useGlobalContextMenuGuard } from "./hooks/useGlobalContextMenuGuard";
import ApplicationRouterProvider from "./router/provider";
import AppQueryClientProvider from "./router/query-client";

function App() {
  // Disable right-click globally except where explicitly allowed
  useGlobalContextMenuGuard();

  // AuthProvider seeds and warms the capability cache, so the shared query
  // client must already be in scope above it.
  return (
    <AppQueryClientProvider>
      <AuthProvider>
        <ApplicationRouterProvider />
      </AuthProvider>
    </AppQueryClientProvider>
  );
}

export default App;
