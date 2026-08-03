import { Icon } from "@iconify/react";
import { useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

import GroupsTab from "./GroupsTab";

const AccountsGroupsPage = () => {
  const [createGroupHandler, setCreateGroupHandler] = useState<
    (() => void) | null
  >(null);
  const [groupsView, setGroupsView] = useViewMode("accounts.groups", "table");

  const actions = (
    <>
      <ViewModeToggle
        alternateMode="table"
        onViewModeChange={setGroupsView}
        viewMode={groupsView}
      />
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
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <GroupsTab
        onMountCreateHandler={(handler) => setCreateGroupHandler(() => handler)}
        viewMode={groupsView}
      />
    </>
  );
};

export default AccountsGroupsPage;
