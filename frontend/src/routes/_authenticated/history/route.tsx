import { createFileRoute } from "@tanstack/react-router";

import { loadRouteTransport } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import HistoryPage from "./-components/HistoryPage";

export const Route = createFileRoute("/_authenticated/history")({
  validateSearch: (search) => ({ ...optionalString(search, "job") }),
  // The page pages through the history itself, so there is no single query
  // for the loader to warm.
  loader: ({ abortController, context }) =>
    loadRouteTransport(context, abortController.signal),
  component: HistoryRoute,
});

function HistoryRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <HistoryPage
      selected={search.job}
      onSelectedChange={(job) =>
        navigate({ to: "/history", search: { job: job ?? undefined } })
      }
    />
  );
}
