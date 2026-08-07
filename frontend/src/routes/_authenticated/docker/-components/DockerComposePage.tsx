import { Icon } from "@iconify/react";
import { useCallback, useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
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
      <ViewModeToggle
        alternateMode="table"
        onViewModeChange={setStacksView}
        viewMode={stacksView}
      />
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
