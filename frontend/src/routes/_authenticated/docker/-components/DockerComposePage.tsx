import { Icon } from "@iconify/react";
import { useCallback, useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useViewMode } from "@/hooks/useViewMode";

import ComposeStacksPage from "./ComposeStacksPage";
import { useDockerUpdateCheck } from "./useDockerUpdateCheck";

const DockerComposePage = () => {
  const { button: checkUpdatesButton } = useDockerUpdateCheck();
  const [stacksView, setStacksView] = useViewMode("docker.stacks", "table");
  const [createStackHandler, setCreateStackHandler] = useState<
    (() => void) | null
  >(null);
  const handleMountCreateStackHandler = useCallback((handler: () => void) => {
    setCreateStackHandler(() => handler);
  }, []);

  const actions = (
    <>
      {checkUpdatesButton}
      <AppTooltip
        title={
          stacksView === "table"
            ? "Switch to card view"
            : "Switch to table view"
        }
      >
        <AppIconButton
          aria-label={
            stacksView === "table"
              ? "Switch to card view"
              : "Switch to table view"
          }
          onClick={() =>
            setStacksView(stacksView === "table" ? "card" : "table")
          }
          size="small"
        >
          {stacksView === "table" ? (
            <Icon height={20} icon="mdi:card-multiple" width={20} />
          ) : (
            <Icon height={20} icon="mdi:table" width={20} />
          )}
        </AppIconButton>
      </AppTooltip>
      {createStackHandler && (
        <AppButton
          onClick={createStackHandler}
          size="small"
          startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
          variant="contained"
        >
          Create Stack
        </AppButton>
      )}
    </>
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <ComposeStacksPage
        onMountCreateHandler={handleMountCreateStackHandler}
        viewMode={stacksView}
      />
    </>
  );
};

export default DockerComposePage;
