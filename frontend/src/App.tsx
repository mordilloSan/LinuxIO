import React from "react";
import { useRoutes } from "react-router-dom";
import { Toaster } from "sonner";

import { AuthProvider } from "./contexts/AuthContext";
import routes from "./routes";
import ReactQueryProvider from "./utils/ReactQueryProvider";

function App() {
  const content = useRoutes(routes);

  return (
    <>
      <AuthProvider>
        <ReactQueryProvider>{content}</ReactQueryProvider>
      </AuthProvider>
      <Toaster richColors position="top-right" />
    </>
  );
}

export default App;
