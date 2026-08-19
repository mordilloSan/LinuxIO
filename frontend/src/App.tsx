import { AuthProvider } from "./contexts/AuthContext";
import { useGlobalContextMenuGuard } from "./hooks/useGlobalContextMenuGuard";
import ApplicationRouterProvider from "./router/provider";

function App() {
  // Disable right-click globally except where explicitly allowed
  useGlobalContextMenuGuard();

  return (
    <AuthProvider>
      <ApplicationRouterProvider />
    </AuthProvider>
  );
}

export default App;
