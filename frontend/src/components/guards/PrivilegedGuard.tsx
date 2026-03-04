import { Alert } from "@mui/material";
import React, { PropsWithChildren } from "react";
import { Navigate } from "react-router-dom";

import PageLoader from "@/components/loaders/PageLoader";
import useAuth from "@/hooks/useAuth";

export const PrivilegedGuard: React.FC<PropsWithChildren> = ({ children }) => {
  const { isAuthenticated, isInitialized, privileged } = useAuth();

  // Wait for initialization
  if (!isInitialized) {
    return <PageLoader />;
  }

  // Not authenticated - redirect to sign-in
  if (!isAuthenticated) {
    return <Navigate to="/sign-in" replace />;
  }

  // Authenticated but not privileged - show error
  if (!privileged) {
    return (
      <div style={{ padding: 16 }}>
        <Alert severity="error">
          Access Denied: This page requires administrator privileges.
        </Alert>
      </div>
    );
  }

  // Privileged user - render children
  return <>{children}</>;
};
