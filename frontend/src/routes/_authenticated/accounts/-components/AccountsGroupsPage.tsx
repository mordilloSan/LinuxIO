import { useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import HeaderActions from "@/components/ui/HeaderActions";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

import GroupsTab from "./GroupsTab";

const AccountsGroupsPage = () => {
  const [createGroupHandler, setCreateGroupHandler] = useState<
    (() => void) | null
  >(null);
  const [groupsView, setGroupsView] = useViewMode("accounts.groups");

  const actions = (
    <HeaderActions
      create={
        createGroupHandler && (
          <AppActionIconButton
            ariaLabel="Add Group"
            icon="mdi:plus"
            iconSize={20}
            label="Add Group"
            onClick={createGroupHandler}
          />
        )
      }
      view={
        <ViewModeToggle
          alternateMode="table"
          onViewModeChange={setGroupsView}
          viewMode={groupsView}
        />
      }
    />
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
