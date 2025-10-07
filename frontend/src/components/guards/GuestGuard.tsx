import React, { PropsWithChildren } from "react";
import { Navigate, useSearchParams } from "react-router-dom";

import useAuth from "@/hooks/useAuth";

export const GuestGuard: React.FC<PropsWithChildren> = ({ children }) => {
  const { isAuthenticated, isInitialized } = useAuth();
  const [searchParams] = useSearchParams();
  const redirect = searchParams.get("redirect") || "/";

  if (isInitialized && isAuthenticated) {
    return <Navigate to={redirect} replace />;
  }

  return <>{children}</>;
};
