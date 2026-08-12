import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalBoolean, optionalString } from "@/routes/-search";

import AccountsUsersPage from "./-components/AccountsUsersPage";

export const Route = createFileRoute("/_authenticated/accounts/")({
  validateSearch: (search) => ({
    ...optionalBoolean(search, "autoDismissFailedLoginAlert"),
    ...optionalString(search, "failedLoginAlertId"),
    ...optionalString(search, "focusLoginEventId"),
    ...optionalString(search, "user"),
  }),
  loaderDeps: ({ search }) => ({ user: search.user }),
  loader: (loaderArgs) => {
    const { deps } = loaderArgs;
    const queries: LoaderQueryOptions[] = [linuxio.accounts.list_users];

    if (deps.user) {
      queries.push(
        linuxio.accounts.get_user_details({ username: deps.user }),
        linuxio.accounts.list_user_logins({ username: deps.user }),
      );
    }

    return loadRouteQueries(loaderArgs, queries);
  },
  component: AccountsUsersPage,
});
