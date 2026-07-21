import { PropsWithChildren } from "react";
import { Navigate, useSearchParams } from "react-router-dom";

import BootstrapLoaderReady from "@/components/loaders/BootstrapLoaderReady";
import useAuth from "@/hooks/useAuth";

export const GuestGuard = ({ children }: PropsWithChildren) => {
  const { isAuthenticated, isInitialized } = useAuth();
  const [searchParams] = useSearchParams();
  const redirect = searchParams.get("redirect") || "/";

  if (!isInitialized) return null;

  if (isAuthenticated) {
    return <Navigate replace to={redirect} />;
  }

  return (
    <>
      {children}
      <BootstrapLoaderReady />
    </>
  );
};
