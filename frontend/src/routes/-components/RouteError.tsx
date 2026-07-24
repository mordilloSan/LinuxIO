import { useQueryErrorResetBoundary } from "@tanstack/react-query";
import { type ErrorComponentProps, useRouter } from "@tanstack/react-router";

import BootstrapLoaderReady from "@/components/loaders/BootstrapLoaderReady";

import ErrorPage from "./ErrorPage";

function RouteError({ error }: ErrorComponentProps) {
  const router = useRouter();
  const queryErrorResetBoundary = useQueryErrorResetBoundary();

  const handleRetry = () => {
    queryErrorResetBoundary.reset();
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
