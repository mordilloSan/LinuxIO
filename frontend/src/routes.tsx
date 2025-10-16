// src/routes/index.tsx
import React, { lazy } from "react";

import { AuthGuard } from "@/components/guards/AuthGuard";
import { GuestGuard } from "@/components/guards/GuestGuard";

// LAZY EVERYTHING that belongs to the protected app:
const MainLayout = lazy(() => import("@/layouts/Main"));
const Default = lazy(() => import("@/pages/main/dashboard"));
const Updates = lazy(() => import("@/pages/main/updates"));
const Docker = lazy(() => import("@/pages/main/docker"));
const Services = lazy(() => import("@/pages/main/services"));
const Network = lazy(() => import("@/pages/main/network"));
const Hardware = lazy(() => import("@/pages/main/hardware"));
const Wireguard = lazy(() => import("@/pages/main/wireguard"));
const Terminal = lazy(() => import("@/pages/main/terminal"));
const Shares = lazy(() => import("@/pages/main/shares"));

// LAZY auth pages too (keeps base bundle small)
const AuthLayout = lazy(() => import("@/layouts/Auth"));
const SignIn = lazy(() => import("@/pages/auth/Login"));
const Page404 = lazy(() => import("@/pages/auth/Page404"));

const routes = [
  // Protected app
  {
    path: "/",
    element: (
      <AuthGuard>
        <MainLayout />
      </AuthGuard>
    ),
    children: [
      { path: "", element: <Default /> },
      { path: "updates", element: <Updates /> },
      { path: "docker", element: <Docker /> },
      { path: "services", element: <Services /> },
      { path: "network", element: <Network /> },
      { path: "shares", element: <Shares /> },
      { path: "hardware", element: <Hardware /> },
      { path: "filebrowser/*", element: <></> },
      { path: "terminal", element: <Terminal /> },
      { path: "wireguard", element: <Wireguard /> },
    ],
  },

  // Auth (public)
  {
    path: "*",
    element: <AuthLayout />,
    children: [
      {
        path: "sign-in",
        element: (
          <GuestGuard>
            <SignIn />
          </GuestGuard>
        ),
      },
      { path: "*", element: <Page404 /> },
    ],
  },
];

export default routes;
