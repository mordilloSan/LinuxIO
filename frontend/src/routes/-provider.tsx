import { useQueryClient } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { useEffect, useMemo, useRef } from "react";

import { isLiveUpdateBlocked } from "@/contexts/UpdateContext";
import useAuth from "@/hooks/useAuth";
import { useAccessContext } from "@/hooks/useCapabilities";
import { router } from "@/router";

function ActiveApplicationRouterProvider() {
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
  const previousContext = useRef(context);

  useEffect(() => {
    if (previousContext.current === context) return;
    previousContext.current = context;
    void router.invalidate();
  }, [context]);

  return <RouterProvider context={context} router={router} />;
}

export default function ApplicationRouterProvider() {
  const { isInitialized } = useAuth();
  return isInitialized ? <ActiveApplicationRouterProvider /> : null;
}
