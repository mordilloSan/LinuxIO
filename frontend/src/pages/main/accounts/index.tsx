import { Icon } from "@iconify/react";
import React, { useState } from "react";
import { useSearchParams } from "react-router-dom";

import { TabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useViewMode } from "@/hooks/useViewMode";

import GroupsTab from "./GroupsTab";
import UsersTab from "./UsersTab";

const AccountsPage: React.FC = () => {
  const [createUserHandler, setCreateUserHandler] = useState<
    (() => void) | null
  >(null);
  const [createGroupHandler, setCreateGroupHandler] = useState<
    (() => void) | null
  >(null);
  const [usersView, setUsersView] = useViewMode("accounts.users", "table");
  const [groupsView, setGroupsView] = useViewMode("accounts.groups", "table");
  const [searchParams] = useSearchParams();
  const isUserDetailOpen = searchParams.get("user") !== null;

  return (
    <TabContainer
      defaultTab="users"
      tabs={[
        {
          value: "users",
          label: "Users",
          component: (
            <UsersTab
              onMountCreateHandler={(handler) =>
                setCreateUserHandler(() => handler)
              }
              viewMode={usersView}
            />
          ),
          rightContent: isUserDetailOpen ? null : (
            <>
              <AppTooltip
                title={
                  usersView === "table"
                    ? "Switch to card view"
                    : "Switch to table view"
                }
              >
                <AppIconButton
                  onClick={() =>
                    setUsersView(usersView === "table" ? "card" : "table")
                  }
                  size="small"
                >
                  {usersView === "table" ? (
                    <Icon height={20} icon="mdi:view-grid" width={20} />
                  ) : (
                    <Icon height={20} icon="mdi:table-row" width={20} />
                  )}
                </AppIconButton>
              </AppTooltip>
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
          ),
        },
        {
          value: "groups",
          label: "Groups",
          component: (
            <GroupsTab
              onMountCreateHandler={(handler) =>
                setCreateGroupHandler(() => handler)
              }
              viewMode={groupsView}
            />
          ),
          rightContent: (
            <>
              <AppTooltip
                title={
                  groupsView === "table"
                    ? "Switch to card view"
                    : "Switch to table view"
                }
              >
                <AppIconButton
                  onClick={() =>
                    setGroupsView(groupsView === "table" ? "card" : "table")
                  }
                  size="small"
                >
                  {groupsView === "table" ? (
                    <Icon height={20} icon="mdi:view-grid" width={20} />
                  ) : (
                    <Icon height={20} icon="mdi:table-row" width={20} />
                  )}
                </AppIconButton>
              </AppTooltip>
              {createGroupHandler && (
                <AppButton
                  onClick={createGroupHandler}
                  size="small"
                  startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
                  variant="contained"
                >
                  Add Group
                </AppButton>
              )}
            </>
          ),
        },
      ]}
      urlParam="accountsTab"
    />
  );
};

export default AccountsPage;
