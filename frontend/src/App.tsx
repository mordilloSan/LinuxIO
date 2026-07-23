import { AuthProvider } from "./contexts/AuthContext";
import { useGlobalContextMenuGuard } from "./hooks/useGlobalContextMenuGuard";
import AppQueryClientProvider from "./routes/-query-client";
import ApplicationRouterProvider from "./routes/-provider";

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
