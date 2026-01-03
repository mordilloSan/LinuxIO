import React, { useEffect } from "react";
import { useRoutes } from "react-router-dom";

import { AuthProvider } from "./contexts/AuthContext";
import ReactQueryProvider from "./contexts/ReactQueryContext";
import { useAppRoutes } from "./routes";

// Inner component that uses React Query hooks
function AppRoutes() {
  const routes = useAppRoutes();
  return useRoutes(routes);
}

function App() {
  // Disable right-click globally except where explicitly allowed
  useEffect(() => {
    const handleContextMenu = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      // Check if the target or any of its parents has the allow-context-menu attribute
      if (!target.closest("[data-allow-context-menu='true']")) {
        e.preventDefault();
      }
    };

    document.addEventListener("contextmenu", handleContextMenu);
    return () => {
      document.removeEventListener("contextmenu", handleContextMenu);
    };
  }, []);

  return (
    <AuthProvider>
      <ReactQueryProvider>
        <AppRoutes />
      </ReactQueryProvider>
    </AuthProvider>
  );
}

export default App;
