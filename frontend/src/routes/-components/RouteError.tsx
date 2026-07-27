import { useQueryErrorResetBoundary } from "@tanstack/react-query";
import { type ErrorComponentProps, useRouter } from "@tanstack/react-router";
import { useEffect } from "react";

import BootstrapLoaderReady from "@/components/loaders/BootstrapLoaderReady";

import ErrorPage from "./ErrorPage";

function RouteError({ error }: ErrorComponentProps) {
  const router = useRouter();
  const queryErrorResetBoundary = useQueryErrorResetBoundary();

  useEffect(() => {
    queryErrorResetBoundary.reset();
  }, [queryErrorResetBoundary]);

  const handleRetry = () => {
    void router.invalidate();
  };

  return (
    <>
      <ErrorPage error={error} onRetry={handleRetry} />
      <BootstrapLoaderReady />
    </>
  );
}

export default RouteError;
