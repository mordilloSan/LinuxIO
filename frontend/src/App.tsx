import React, { useEffect } from "react";
import { useRoutes } from "react-router-dom";
import { Toaster } from "sonner";

import { AuthProvider } from "./contexts/AuthContext";
import { useAppRoutes } from "./routes";
import ReactQueryProvider from "./utils/ReactQueryProvider";
import { ToastHistorySync } from "./utils/toastHistory";

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
    <>
      <AuthProvider>
        <ReactQueryProvider>
          <AppRoutes />
        </ReactQueryProvider>
      </AuthProvider>
      <ToastHistorySync />
      <Toaster
        richColors
        position="top-right"
        toastOptions={{ duration: 1500 }}
      />
    </>
  );
}

export default App;
