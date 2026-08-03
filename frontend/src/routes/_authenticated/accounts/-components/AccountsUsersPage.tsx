import { Icon } from "@iconify/react";
import { getRouteApi } from "@tanstack/react-router";
import { useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

import UsersTab from "./UsersTab";

const accountsUsersRouteApi = getRouteApi("/_authenticated/accounts/");

const AccountsUsersPage = () => {
  const [createUserHandler, setCreateUserHandler] = useState<
    (() => void) | null
  >(null);
  const [usersView, setUsersView] = useViewMode("accounts.users", "table");
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
        <AppButton
          onClick={createUserHandler}
          size="small"
          startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
          variant="contained"
        >
          Add User
        </AppButton>
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
