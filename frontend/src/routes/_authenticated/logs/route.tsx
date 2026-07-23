import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { FileTextIcon } from "@/icons/svg";
import { loadRouteQueries } from "@/routes/-loader";

import GeneralLogsPage from "./-components/GeneralLogsPage";

export const Route = createFileRoute("/_authenticated/logs")({
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.systemd.list_services.queryOptions(),
    ]),
  component: GeneralLogsPage,
  staticData: {
    navigation: {
      icon: FileTextIcon,
      position: 35,
      title: "Logs",
    },
  },
});
