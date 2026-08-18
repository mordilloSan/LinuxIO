import { useCallback, useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import HeaderActions from "@/components/ui/HeaderActions";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

import ComposeStacksPage from "./ComposeStacksPage";
import { useDockerUpdateCheck } from "./useDockerUpdateCheck";

const DockerComposePage = () => {
  const { button: checkUpdatesButton } = useDockerUpdateCheck();
  const [stacksView, setStacksView] = useViewMode("docker.stacks");
  const [createStackHandler, setCreateStackHandler] = useState<
    (() => void) | null
  >(null);
  const handleMountCreateStackHandler = useCallback((handler: () => void) => {
    setCreateStackHandler(() => handler);
  }, []);

  const actions = (
    <HeaderActions
      create={
        createStackHandler && (
          <AppActionIconButton
            ariaLabel="Create Stack"
            icon="mdi:plus"
            iconSize={20}
            label="Create Stack"
            onClick={createStackHandler}
          />
        )
      }
      refresh={checkUpdatesButton}
      view={
        <ViewModeToggle
          alternateMode="table"
          onViewModeChange={setStacksView}
          viewMode={stacksView}
        />
      }
    />
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
