import { useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
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
        <AppActionIconButton
          ariaLabel="Add Group"
          icon="mdi:plus"
          iconSize={20}
          label="Add Group"
          onClick={createGroupHandler}
        />
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
