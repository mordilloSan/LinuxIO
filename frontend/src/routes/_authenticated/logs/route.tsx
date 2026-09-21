import { createFileRoute } from "@tanstack/react-router";

import { FileTextIcon } from "@/icons/svg";
import { loadRouteTransport } from "@/routes/-loader";

import GeneralLogsPage from "./-components/GeneralLogsPage";

const managedServiceUnit = /^[A-Za-z0-9:_.@%-]+\.service$/;
const invocationID = /^[0-9a-f]{32}$/;

const optionalManagedLogScope = (search: Record<string, unknown>) => {
  const unit = typeof search.unit === "string" ? search.unit : "";
  const invocationId =
    typeof search.invocationId === "string" ? search.invocationId : "";
  if (
    (search.unit !== undefined && typeof search.unit !== "string") ||
    (search.invocationId !== undefined &&
      typeof search.invocationId !== "string") ||
    (unit !== "" && (unit.length > 255 || !managedServiceUnit.test(unit))) ||
    (invocationId !== "" && !invocationID.test(invocationId)) ||
    (invocationId !== "" && unit === "")
  ) {
    throw new Error("Invalid scheduled log scope");
  }
  return {
    ...(unit ? { unit } : {}),
    ...(invocationId ? { invocationId } : {}),
  };
};

function LogsRoute() {
  const { unit, invocationId } = Route.useSearch();
  return (
    <GeneralLogsPage
      invocationId={invocationId}
      key={`${unit ?? ""}:${invocationId ?? ""}`}
      unit={unit}
    />
  );
}

export const Route = createFileRoute("/_authenticated/logs")({
  validateSearch: optionalManagedLogScope,
  loader: ({ abortController, context }) =>
    loadRouteTransport(context, abortController.signal),
  component: LogsRoute,
  staticData: {
    navigation: {
      icon: FileTextIcon,
      position: 35,
      title: "Logs",
    },
  },
});
