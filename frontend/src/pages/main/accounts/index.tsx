import { Icon } from "@iconify/react";
import React, { useState } from "react";

import GroupsTab from "./GroupsTab";
import UsersTab from "./UsersTab";

import { TabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useViewMode } from "@/hooks/useViewMode";

const AccountsPage: React.FC = () => {
  const [createUserHandler, setCreateUserHandler] = useState<
    (() => void) | null
  >(null);
  const [createGroupHandler, setCreateGroupHandler] = useState<
    (() => void) | null
  >(null);
  const [usersView, setUsersView] = useViewMode("accounts.users", "table");
  const [groupsView, setGroupsView] = useViewMode("accounts.groups", "table");

  return (
    <TabContainer
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
          rightContent: (
            <>
              <AppTooltip
                title={
                  usersView === "table"
                    ? "Switch to card view"
                    : "Switch to table view"
                }
              >
                <AppIconButton
                  size="small"
                  onClick={() =>
                    setUsersView(usersView === "table" ? "card" : "table")
                  }
                >
                  {usersView === "table" ? (
                    <Icon icon="mdi:view-grid" width={20} height={20} />
                  ) : (
                    <Icon icon="mdi:table-row" width={20} height={20} />
                  )}
                </AppIconButton>
              </AppTooltip>
              {createUserHandler && (
                <AppButton
                  variant="contained"
                  size="small"
                  onClick={createUserHandler}
                  startIcon={<Icon icon="mdi:plus" width={20} height={20} />}
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
                  size="small"
                  onClick={() =>
                    setGroupsView(groupsView === "table" ? "card" : "table")
                  }
                >
                  {groupsView === "table" ? (
                    <Icon icon="mdi:view-grid" width={20} height={20} />
                  ) : (
                    <Icon icon="mdi:table-row" width={20} height={20} />
                  )}
                </AppIconButton>
              </AppTooltip>
              {createGroupHandler && (
                <AppButton
                  variant="contained"
                  size="small"
                  onClick={createGroupHandler}
                  startIcon={<Icon icon="mdi:plus" width={20} height={20} />}
                >
                  Add Group
                </AppButton>
              )}
            </>
          ),
        },
      ]}
      defaultTab="users"
      urlParam="accountsTab"
    />
  );
};

export default AccountsPage;
