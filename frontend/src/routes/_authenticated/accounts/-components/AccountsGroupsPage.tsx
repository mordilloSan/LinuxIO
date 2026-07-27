import { Icon } from "@iconify/react";
import { useState } from "react";

import { RoutedTabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useViewMode } from "@/hooks/useViewMode";

import { ACCOUNTS_TABS } from "./accountsTabs";
import GroupsTab from "./GroupsTab";

const AccountsGroupsPage = () => {
  const [createGroupHandler, setCreateGroupHandler] = useState<
    (() => void) | null
  >(null);
  const [groupsView, setGroupsView] = useViewMode("accounts.groups", "table");

  const actions = (
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
            <Icon height={20} icon="mdi:card-multiple" width={20} />
          ) : (
            <Icon height={20} icon="mdi:table" width={20} />
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
  );

  return (
    <RoutedTabContainer rightContent={actions} tabs={ACCOUNTS_TABS}>
      <GroupsTab
        onMountCreateHandler={(handler) => setCreateGroupHandler(() => handler)}
        viewMode={groupsView}
      />
    </RoutedTabContainer>
  );
};

export default AccountsGroupsPage;
