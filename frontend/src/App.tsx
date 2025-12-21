import React, { useEffect } from "react";
import { useRoutes } from "react-router-dom";
import { Toaster } from "sonner";

import { AuthProvider } from "./contexts/AuthContext";
import routes from "./routes";
import { ToastHistorySync } from "./utils/toastHistory";
import ReactQueryProvider from "./utils/ReactQueryProvider";

function App() {
  const content = useRoutes(routes);

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
        <ReactQueryProvider>{content}</ReactQueryProvider>
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
