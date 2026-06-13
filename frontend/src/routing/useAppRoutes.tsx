import React, { useMemo } from "react";

import { AuthGuard } from "@/components/guards/AuthGuard";
import { GuestGuard } from "@/components/guards/GuestGuard";
import { hasAccessPolicy, useAccessContext } from "@/hooks/useCapabilities";
import { AuthLayout, MainLayout, Page404, SignIn, coreRoutes } from "@/routes";

export function useAppRoutes() {
  const access = useAccessContext();

  return useMemo(() => {
    const allProtectedRoutes = coreRoutes.filter((route) =>
      hasAccessPolicy(route, access),
    );

    return [
      {
        path: "/",
        element: (
          <AuthGuard>
            <MainLayout />
          </AuthGuard>
        ),
        children: [...allProtectedRoutes, { path: "*", element: <Page404 /> }],
      },
      {
        path: "/sign-in",
        element: <AuthLayout />,
        children: [
          {
            index: true,
            element: (
              <GuestGuard>
                <SignIn />
              </GuestGuard>
            ),
          },
        ],
      },
    ];
  }, [access]);
}
