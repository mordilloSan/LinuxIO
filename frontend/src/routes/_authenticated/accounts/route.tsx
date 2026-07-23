import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { UsersIcon } from "@/icons/svg";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalBoolean, optionalString } from "@/routes/-search";

import AccountsPage from "./-components/AccountsPage";

export const Route = createFileRoute("/_authenticated/accounts")({
  validateSearch: (search) => ({
    ...optionalString(search, "accountsTab"),
    ...optionalBoolean(search, "autoDismissFailedLoginAlert"),
    ...optionalString(search, "failedLoginAlertId"),
    ...optionalString(search, "focusLoginEventId"),
    ...optionalString(search, "user"),
  }),
  loaderDeps: ({ search }) => ({
    accountsTab: search.accountsTab,
    user: search.user,
  }),
  loader: ({ context, deps, preload }) => {
    const queries: LoaderQueryOptions[] =
      deps.accountsTab === "groups"
        ? [linuxio.accounts.list_groups.queryOptions()]
        : [linuxio.accounts.list_users.queryOptions()];

    if (deps.accountsTab !== "groups" && deps.user) {
      queries.push(
        linuxio.accounts.get_user_details.queryOptions(deps.user),
        linuxio.accounts.list_user_logins.queryOptions(deps.user),
      );
    }

    return loadRouteQueries({ context, preload }, queries);
  },
  component: AccountsPage,
  staticData: {
    navigation: {
      icon: UsersIcon,
      position: 60,
      title: "Accounts",
    },
  },
});
