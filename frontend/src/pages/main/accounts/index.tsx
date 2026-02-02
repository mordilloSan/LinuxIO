import { Add as AddIcon } from "@mui/icons-material";
import { Box, Button } from "@mui/material";
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
              sx={{
                minWidth: { xs: "40px", sm: "auto" },
                px: { xs: 1, sm: 2 },
                "& .MuiButton-startIcon": {
                  display: { xs: "none", sm: "flex" },
                  margin: { xs: 0, sm: "0 8px 0 -4px" },
                },
              }}
              startIcon={<AddIcon />}
            >
              <Box
                sx={{
                  display: { xs: "none", sm: "flex" },
                  alignItems: "center",
                }}
              >
                Add User
              </Box>
              <AddIcon sx={{ display: { xs: "block", sm: "none" } }} />
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
              sx={{
                minWidth: { xs: "40px", sm: "auto" },
                px: { xs: 1, sm: 2 },
                "& .MuiButton-startIcon": {
                  display: { xs: "none", sm: "flex" },
                  margin: { xs: 0, sm: "0 8px 0 -4px" },
                },
              }}
              startIcon={<AddIcon />}
            >
              <Box
                sx={{
                  display: { xs: "none", sm: "flex" },
                  alignItems: "center",
                }}
              >
                Add Group
              </Box>
              <AddIcon sx={{ display: { xs: "block", sm: "none" } }} />
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
