import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo, useRef, useState } from "react";

import { linuxio, type ContainerInfo } from "@/api";
import PruneDialog, {
  type PruneOptions,
} from "@/components/docker/PruneDialog";
import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import { useScopedToast } from "@/hooks/useScopedToast";

import DockerDashboard from "./DockerDashboard";
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
    linuxio.docker.start_all_stopped.useAction({
      success: (result) => {
        toast.success(`Started ${result.started} container(s)`);
      },
      error: "Failed to start containers",
      toast: DOCKER_TOAST_META,
    });
  const { mutateAsync: stopContainer } =
    linuxio.docker.stop_container.useAction();
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
    linuxio.docker.system_prune.useAction({
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
      <AppActionIconButton
        ariaLabel="Start All"
        disabled={
          containersFetching || isStartingAll || stoppedContainers.length === 0
        }
        icon="mdi:play"
        iconSize={20}
        label="Start All"
        loading={isStartingAll}
        onClick={() => startAllStopped()}
      />
      <AppActionIconButton
        ariaLabel="Stop All"
        disabled={
          containersFetching || isStoppingAll || runningContainers.length === 0
        }
        icon="mdi:stop"
        iconSize={20}
        label="Stop All"
        loading={isStoppingAll}
        onClick={() => void handleStopAllRunning()}
      />
      <AppActionIconButton
        ariaLabel="Prune All"
        disabled={isPruning}
        icon="mdi:broom"
        iconSize={20}
        label="Prune All"
        loading={isPruning}
        onClick={() => setPruneDialogOpen(true)}
      />
    </>
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <DockerDashboard stoppingContainerIds={stoppingContainerIds} />
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
