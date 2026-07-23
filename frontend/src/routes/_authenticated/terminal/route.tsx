import { createFileRoute } from "@tanstack/react-router";

import { TerminalIcon } from "@/icons/svg";
import { loadRouteTransport } from "@/routes/-loader";

import Terminal from "./-components/Terminal";

export const Route = createFileRoute("/_authenticated/terminal")({
  loader: ({ context }) => loadRouteTransport(context),
  component: Terminal,
  staticData: {
    navigation: {
      icon: TerminalIcon,
      position: 110,
      title: "Terminal",
    },
  },
});
