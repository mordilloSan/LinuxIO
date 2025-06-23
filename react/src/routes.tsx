import React, { lazy } from "react";

// Guards & Layouts
import { WebSocketProvider } from "./contexts/WebSocketContext";

import { AuthGuard } from "@/components/guards/AuthGuard";
import { GuestGuard } from "@/components/guards/GuestGuard";
import AuthLayout from "@/layouts/Auth";
import MainLayout from "@/layouts/Main";
import Default from "@/pages/dashboard/home";

// Lazy-loaded pages
const SignIn = lazy(() => import("@/pages/auth/SignIn"));
const Page404 = lazy(() => import("@/pages/auth/Page404"));
const Updates = lazy(() => import("@/pages/dashboard/updates"));
const Docker = lazy(() => import("@/pages/dashboard/docker"));
const Services = lazy(() => import("@/pages/dashboard/services"));
const Network = lazy(() => import("@/pages/dashboard/network"));
const Hardware = lazy(() => import("@/pages/dashboard/hardware"));
const Filebrowser = lazy(
  () => import("@/pages/dashboard/filebrowser/FilebrowserIframe"),
);

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
