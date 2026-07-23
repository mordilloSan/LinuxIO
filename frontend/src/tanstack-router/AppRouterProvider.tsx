import { useQueryClient } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { useEffect, useMemo, useState } from "react";

import { isLiveUpdateBlocked } from "@/contexts/UpdateContext";
import useAuth from "@/hooks/useAuth";
import { useAccessContext } from "@/hooks/useCapabilities";

import { createApplicationRouter } from "./appRouteRegistry";

function ActiveAppRouterProvider() {
  const auth = useAuth();
  const access = useAccessContext();
  const queryClient = useQueryClient();
  const context = useMemo(
    () => ({
      access,
      auth: {
        isAuthenticated: auth.isAuthenticated,
        isInitialized: true as const,
        user: auth.user,
      },
      isUpdateBlocked: isLiveUpdateBlocked,
      queryClient,
    }),
    [access, auth.isAuthenticated, auth.user, queryClient],
  );
  const [router] = useState(() => createApplicationRouter(context));

  useEffect(() => {
    router.update({ context });
    void router.invalidate();
  }, [context, router]);

  return <RouterProvider context={context} router={router} />;
}

export default function AppRouterProvider() {
  const { isInitialized } = useAuth();
  return isInitialized ? <ActiveAppRouterProvider /> : null;
}
