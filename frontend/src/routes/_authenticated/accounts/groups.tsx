import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import AccountsGroupsPage from "./-components/AccountsGroupsPage";

export const Route = createFileRoute("/_authenticated/accounts/groups")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.accounts.list_groups]),
  component: AccountsGroupsPage,
});
