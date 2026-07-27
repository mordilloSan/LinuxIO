import { Icon } from "@iconify/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo, useRef, useState } from "react";

import { linuxio, type ContainerInfo } from "@/api";
import PruneDialog, {
  type PruneOptions,
} from "@/components/docker/PruneDialog";
import { RoutedTabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import { useScopedToast } from "@/hooks/useScopedToast";

import DockerDashboard from "./DockerDashboard";
import { DOCKER_TABS } from "./dockerTabs";
import { useDockerUpdateCheck } from "./useDockerUpdateCheck";

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

const getContainerName = (container: ContainerInfo) =>
  container.Names?.[0]?.replace("/", "") || "Unnamed";

const DockerDashboardPage = () => {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const { button: checkUpdatesButton } = useDockerUpdateCheck();
  const [pruneDialogOpen, setPruneDialogOpen] = useState(false);
  const stopAllInFlightRef = useRef(false);
  const [stoppingContainerIds, setStoppingContainerIds] = useState<Set<string>>(
    () => new Set(),
  );
  const { data: containers, isFetching: containersFetching } = useSuspenseQuery(
    linuxio.docker.list_containers.queryOptions({
      refetchInterval: 5000,
    }),
  );
  const stoppedContainers = useMemo(
    () => containers.filter((c) => c.State === "exited" || c.State === "dead"),
    [containers],
  );
  const runningContainers = useMemo(
    () => containers.filter((c) => c.State === "running"),
    [containers],
  );
  const { mutate: startAllStopped, isPending: isStartingAll } =
    linuxio.docker.start_all_stopped.useJobAction({
      success: (result) => {
        toast.success(`Started ${result.started} container(s)`);
      },
      error: "Failed to start containers",
      toast: DOCKER_TOAST_META,
    });
  const { mutateAsync: stopContainer } =
    linuxio.docker.stop_container.useJobAction();
  const isStoppingAll = stoppingContainerIds.size > 0;
  const handleStopAllRunning = async () => {
    if (stopAllInFlightRef.current || runningContainers.length === 0) return;

    const targets = [...runningContainers];
    stopAllInFlightRef.current = true;
    setStoppingContainerIds(new Set(targets.map((container) => container.Id)));

    const failures: string[] = [];
    for (const container of targets) {
      try {
        await stopContainer({ containerId: container.Id });
      } catch {
        failures.push(getContainerName(container));
      }
      setStoppingContainerIds((previous) => {
        const next = new Set(previous);
        next.delete(container.Id);
        return next;
      });
    }
    stopAllInFlightRef.current = false;
    setStoppingContainerIds(new Set());

    if (failures.length > 0) {
      toast.error(
        `Failed to stop ${failures.length} of ${targets.length} container${targets.length === 1 ? "" : "s"}`,
      );
    } else {
      toast.success(`Stopped ${targets.length} container(s)`);
    }
  };
  const { mutate: systemPrune, isPending: isPruning } =
    linuxio.docker.system_prune.useJobAction({
      success: () => {
        toast.success("Docker prune completed");
        setPruneDialogOpen(false);
      },
      error: "Prune failed",
      toast: DOCKER_TOAST_META,
    });

  const actions = (
    <>
      {checkUpdatesButton}
      <AppButton
        disabled={
          containersFetching || isStartingAll || stoppedContainers.length === 0
        }
        onClick={() => startAllStopped()}
        size="small"
        startIcon={
          isStartingAll ? (
            <AppCircularProgress color="inherit" size={18} />
          ) : (
            <Icon height={20} icon="mdi:play" width={20} />
          )
        }
        variant="outlined"
      >
        Start All
      </AppButton>
      <AppButton
        color="warning"
        disabled={
          containersFetching || isStoppingAll || runningContainers.length === 0
        }
        onClick={() => void handleStopAllRunning()}
        size="small"
        startIcon={
          isStoppingAll ? (
            <AppCircularProgress color="inherit" size={18} />
          ) : (
            <Icon height={20} icon="mdi:stop" width={20} />
          )
        }
        variant="outlined"
      >
        Stop All
      </AppButton>
      <AppButton
        color="error"
        disabled={isPruning}
        onClick={() => setPruneDialogOpen(true)}
        size="small"
        startIcon={<Icon height={20} icon="mdi:broom" width={20} />}
        variant="outlined"
      >
        Prune All
      </AppButton>
    </>
  );

  return (
    <>
      <RoutedTabContainer rightContent={actions} tabs={DOCKER_TABS}>
        <DockerDashboard stoppingContainerIds={stoppingContainerIds} />
      </RoutedTabContainer>
      <PruneDialog
        isLoading={isPruning}
        onClose={() => !isPruning && setPruneDialogOpen(false)}
        onConfirm={(options: PruneOptions) => systemPrune(options)}
        open={pruneDialogOpen}
      />
    </>
  );
};

export default DockerDashboardPage;
