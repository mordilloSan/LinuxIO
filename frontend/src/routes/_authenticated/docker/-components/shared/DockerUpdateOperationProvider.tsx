import { createContext, type ReactNode, useContext, useState } from "react";

import {
  linuxio,
  type DockerContainerUpdateProgress,
  type DockerContainerUpdateRequest,
  type DockerContainerUpdateResult,
  type TaskProgress,
  useStreamMux,
} from "@/api";
import useAuth from "@/hooks/useAuth";
import { useScopedToast } from "@/hooks/useScopedToast";
import { TASK_TYPE_DOCKER_UPDATE } from "@/tasks/taskTypes";
import { useActiveTaskRecovery } from "@/tasks/useActiveTaskRecovery";

import { DOCKER_TOAST_META } from "./dockerToast";
import {
  DockerUpdateOperationDialog,
  type DockerUpdateEvent,
  type DockerUpdateTarget,
} from "./DockerUpdateOperationDialog";

interface DockerUpdateOperationContextValue {
  /** The update route is privileged; a non-sudo session cannot run it. */
  canUpdate: boolean;
  isUpdating: (containerId: string) => boolean;
  /** Rebuilds the image of a Compose service built on the host, then updates
   * the container to it. */
  startRebuild: (containerId: string, containerName: string) => void;
  startUpdate: (containerId: string, containerName: string) => void;
  updating: boolean;
}

const DockerUpdateOperationContext =
  createContext<DockerUpdateOperationContextValue>({
    canUpdate: false,
    isUpdating: () => false,
    startRebuild: () => undefined,
    startUpdate: () => undefined,
    updating: false,
  });

interface DockerUpdateLog {
  /** The build's output lines, the newest last. */
  buildOutput: string[];
  events: DockerUpdateEvent[];
}

const EMPTY_LOG: DockerUpdateLog = { buildOutput: [], events: [] };

/** How many of the build's last lines the dialog keeps. */
const MAX_BUILD_OUTPUT_LINES = 300;

/** The phase of each line a rebuild's build writes (dockerupdate's
 * ProgressPhaseBuildOutput): output for the log block, not a step row. */
const BUILD_OUTPUT_PHASE = "build_output";

// Build output goes to the log block, which keeps the newest lines only;
// every other phase is a row, one per message.
const appendProgress = (
  log: DockerUpdateLog,
  phase: string,
  message: string,
): DockerUpdateLog => {
  if (phase === BUILD_OUTPUT_PHASE) {
    return {
      ...log,
      buildOutput: [
        ...log.buildOutput.slice(-(MAX_BUILD_OUTPUT_LINES - 1)),
        message,
      ],
    };
  }
  const last = log.events[log.events.length - 1];
  if (last?.phase === phase && last.message === message) return log;
  return { ...log, events: [...log.events, { message, phase }] };
};

export const useDockerUpdateOperation = () =>
  useContext(DockerUpdateOperationContext);

export function DockerUpdateOperationProvider({
  children,
}: {
  children: ReactNode;
}) {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const { privileged } = useAuth();
  const [target, setTarget] = useState<DockerUpdateTarget | null>(null);
  const [log, setLog] = useState<DockerUpdateLog>(EMPTY_LOG);
  const [result, setResult] = useState<DockerContainerUpdateResult | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const { isOpen: muxIsOpen } = useStreamMux();

  const operation = linuxio.docker.update_container.useMutation({
    error: (streamError) => {
      if (streamError.name === "AbortError") return;
      setError(streamError.message || "Container update failed");
    },
    onProgress: (progress: TaskProgress<DockerContainerUpdateProgress>) => {
      const detail = progress.detail;
      const phase = detail?.phase || progress.phase || "running";
      const message = detail?.message || progress.message;
      if (!message) return;
      setLog((previous) => appendProgress(previous, phase, message));
    },
    success: (updateResult) => {
      setResult(updateResult);
      setError(null);
    },
  });
  const { isPending, mutate, watch } = operation;

  const begin = (
    nextTarget: DockerUpdateTarget,
    request: DockerContainerUpdateRequest,
  ) => {
    setTarget(nextTarget);
    setLog(EMPTY_LOG);
    setResult(null);
    setError(null);
    setOpen(true);
    mutate(request);
  };

  const startUpdate = (containerId: string, containerName: string) => {
    if (isPending) {
      setOpen(true);
      return;
    }
    begin({ id: containerId, name: containerName }, { containerId });
  };

  const startRebuild = (containerId: string, containerName: string) => {
    if (isPending) {
      setOpen(true);
      return;
    }
    begin(
      { id: containerId, name: containerName, rebuild: true },
      { containerId, rebuild: true },
    );
  };

  useActiveTaskRecovery({
    type: TASK_TYPE_DOCKER_UPDATE,
    scanKey: muxIsOpen ? "docker-update" : null,
    match: () => true,
    onRecover: (task) => {
      const containerId = task.metadata?.identity?.[0];
      if (!containerId || isPending) return;
      const recoveredTarget = {
        id: containerId,
        name: containerId.slice(0, 12),
      };
      setTarget(recoveredTarget);
      setLog(EMPTY_LOG);
      setResult(null);
      setError(null);
      setOpen(true);
      watch(task.id, { containerId });
    },
  });

  const handleClose = () => {
    setOpen(false);
    if (isPending) {
      toast.info(
        target?.rebuild
          ? "Container rebuild is still running in the background"
          : "Container update is still running in the background",
      );
    }
  };

  const value: DockerUpdateOperationContextValue = {
    canUpdate: privileged,
    isUpdating: (containerId) => isPending && target?.id === containerId,
    startRebuild,
    startUpdate,
    updating: isPending,
  };

  return (
    <DockerUpdateOperationContext.Provider value={value}>
      {children}
      <DockerUpdateOperationDialog
        buildOutput={log.buildOutput}
        error={error}
        events={log.events}
        onClose={handleClose}
        open={open}
        result={result}
        running={isPending}
        target={target}
      />
    </DockerUpdateOperationContext.Provider>
  );
}
