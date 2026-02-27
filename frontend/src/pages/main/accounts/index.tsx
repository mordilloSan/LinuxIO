import {
  Add as AddIcon,
  GridView as GridViewIcon,
  TableRows as TableRowsIcon,
} from "@mui/icons-material";
import { Button, IconButton, Tooltip } from "@mui/material";
import React, { useState } from "react";

import GroupsTab from "./GroupsTab";
import UsersTab from "./UsersTab";

import { TabContainer } from "@/components/tabbar";
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
              <Tooltip
                title={
                  usersView === "table"
                    ? "Switch to card view"
                    : "Switch to table view"
                }
              >
                <IconButton
                  size="small"
                  onClick={() =>
                    setUsersView(usersView === "table" ? "card" : "table")
                  }
                >
                  {usersView === "table" ? (
                    <GridViewIcon fontSize="small" />
                  ) : (
                    <TableRowsIcon fontSize="small" />
                  )}
                </IconButton>
              </Tooltip>
              {createUserHandler && (
                <Button
                  variant="contained"
                  size="small"
                  onClick={createUserHandler}
                  startIcon={<AddIcon />}
                >
                  Add User
                </Button>
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
              <Tooltip
                title={
                  groupsView === "table"
                    ? "Switch to card view"
                    : "Switch to table view"
                }
              >
                <IconButton
                  size="small"
                  onClick={() =>
                    setGroupsView(groupsView === "table" ? "card" : "table")
                  }
                >
                  {groupsView === "table" ? (
                    <GridViewIcon fontSize="small" />
                  ) : (
                    <TableRowsIcon fontSize="small" />
                  )}
                </IconButton>
              </Tooltip>
              {createGroupHandler && (
                <Button
                  variant="contained"
                  size="small"
                  onClick={createGroupHandler}
                  startIcon={<AddIcon />}
                >
                  Add Group
                </Button>
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
