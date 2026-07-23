import { AuthProvider } from "./contexts/AuthContext";
import ReactQueryProvider from "./contexts/ReactQueryContext";
import { useGlobalContextMenuGuard } from "./hooks/useGlobalContextMenuGuard";
import AppRouterProvider from "./tanstack-router/AppRouterProvider";

function App() {
  // Disable right-click globally except where explicitly allowed
  useGlobalContextMenuGuard();

  return (
    <AuthProvider>
      <ReactQueryProvider>
        <AppRouterProvider />
      </ReactQueryProvider>
    </AuthProvider>
  );
}

export default App;
