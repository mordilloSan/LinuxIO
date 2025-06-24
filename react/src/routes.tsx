import React, { lazy } from "react";

// Guards & Layouts
import { WebSocketProvider } from "./contexts/WebSocketContext";

import { AuthGuard } from "@/components/guards/AuthGuard";
import { GuestGuard } from "@/components/guards/GuestGuard";
import AuthLayout from "@/layouts/Auth";
import MainLayout from "@/layouts/Main";
import Default from "@/pages/main/dashboard";
import Filebrowser from "@/pages/main/filebrowser";

// Lazy-loaded pages
const SignIn = lazy(() => import("@/pages/auth/Login"));
const Page404 = lazy(() => import("@/pages/auth/Page404"));
const Updates = lazy(() => import("@/pages/main/updates"));
const Docker = lazy(() => import("@/pages/main/docker"));
const Services = lazy(() => import("@/pages/main/services"));
const Network = lazy(() => import("@/pages/main/network"));
const Hardware = lazy(() => import("@/pages/main/hardware"));
const Terminal = lazy(() => import("@/pages/main/terminal"));

// Route config
const routes = [
  {
    path: "/",
    element: (
      <AuthGuard>
        <WebSocketProvider>
          <MainLayout />
        </WebSocketProvider>
      </AuthGuard>
    ),
    children: [
      { path: "", element: <Default /> },
      { path: "updates", element: <Updates /> },
      { path: "docker", element: <Docker /> },
      { path: "services", element: <Services /> },
      { path: "network", element: <Network /> },
      { path: "hardware", element: <Hardware /> },
      { path: "filebrowser", element: <Filebrowser /> },
      { path: "terminal", element: <Terminal /> },
    ],
  },
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

function extractChannels(routeList: any[]): string[] {
  let channels: string[] = [];
  for (const route of routeList) {
    if (route.path !== "*") {
      let channel = route.path === "" ? "dashboard" : route.path;
      channels.push(channel);
    }
    if (route.children) {
      channels = channels.concat(extractChannels(route.children));
    }
  }
  return channels;
}

export const ROUTE_CHANNELS = extractChannels(routes);
