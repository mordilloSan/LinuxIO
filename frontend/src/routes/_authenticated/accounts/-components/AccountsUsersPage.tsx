import { getRouteApi } from "@tanstack/react-router";
import { useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

import UsersTab from "./UsersTab";

const accountsUsersRouteApi = getRouteApi("/_authenticated/accounts/");

const AccountsUsersPage = () => {
  const [createUserHandler, setCreateUserHandler] = useState<
    (() => void) | null
  >(null);
  const [usersView, setUsersView] = useViewMode("accounts.users");
  const search = accountsUsersRouteApi.useSearch();
  const isUserDetailOpen = typeof search.user === "string";

  const actions = isUserDetailOpen ? null : (
    <>
      <ViewModeToggle
        alternateMode="table"
        onViewModeChange={setUsersView}
        viewMode={usersView}
      />
      {createUserHandler && (
        <AppActionIconButton
          ariaLabel="Add User"
          icon="mdi:plus"
          iconSize={20}
          label="Add User"
          onClick={createUserHandler}
        />
      )}
    </>
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <UsersTab
        onMountCreateHandler={(handler) => setCreateUserHandler(() => handler)}
        viewMode={usersView}
      />
    </>
  );
};

export default AccountsUsersPage;
