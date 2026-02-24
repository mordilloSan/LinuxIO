import { Add as AddIcon } from "@mui/icons-material";
import { Button } from "@mui/material";
import React, { useState } from "react";

import GroupsTab from "./GroupsTab";
import UsersTab from "./UsersTab";

import { TabContainer } from "@/components/tabbar";

const AccountsPage: React.FC = () => {
  const [createUserHandler, setCreateUserHandler] = useState<
    (() => void) | null
  >(null);
  const [createGroupHandler, setCreateGroupHandler] = useState<
    (() => void) | null
  >(null);

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
            />
          ),
          rightContent: createUserHandler ? (
            <Button
              variant="contained"
              size="small"
              onClick={createUserHandler}
              startIcon={<AddIcon />}
            >
              Add User
            </Button>
          ) : undefined,
        },
        {
          value: "groups",
          label: "Groups",
          component: (
            <GroupsTab
              onMountCreateHandler={(handler) =>
                setCreateGroupHandler(() => handler)
              }
            />
          ),
          rightContent: createGroupHandler ? (
            <Button
              variant="contained"
              size="small"
              onClick={createGroupHandler}
              startIcon={<AddIcon />}
            >
              Add Group
            </Button>
          ) : undefined,
        },
      ]}
      defaultTab="users"
      urlParam="accountsTab"
    />
  );
};

export default AccountsPage;
