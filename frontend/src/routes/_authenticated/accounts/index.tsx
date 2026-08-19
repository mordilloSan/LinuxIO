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
  context: ({ deps }) => ({
    selectedUsername: deps.user,
    listUsersQueryOptions: linuxio.accounts.list_users,
    selectedUserDetailsQueryOptions: deps.user
      ? linuxio.accounts.get_user_details({ username: deps.user })
      : undefined,
    selectedUserLoginsQueryOptions: deps.user
      ? linuxio.accounts.list_user_logins({ username: deps.user })
      : undefined,
  }),
  loader: (loaderArgs) => {
    const {
      listUsersQueryOptions,
      selectedUserDetailsQueryOptions,
      selectedUserLoginsQueryOptions,
    } = loaderArgs.context;
    const queries: LoaderQueryOptions[] = [listUsersQueryOptions];

    if (selectedUserDetailsQueryOptions && selectedUserLoginsQueryOptions) {
      queries.push(
        selectedUserDetailsQueryOptions,
        selectedUserLoginsQueryOptions,
      );
    }

    return loadRouteQueries(loaderArgs, queries);
  },
  component: AccountsUsersPage,
});
