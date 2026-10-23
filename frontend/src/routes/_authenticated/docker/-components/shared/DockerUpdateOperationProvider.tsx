import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import {
  linuxio,
  type DockerContainerUpdateProgress,
  type DockerContainerUpdateRequest,
  type DockerContainerUpdateResult,
  type Stream,
  type TaskProgress,
  useStreamMux,
} from "@/api";
import { useScopedToast } from "@/hooks/useScopedToast";
import { TASK_TYPE_DOCKER_UPDATE } from "@/tasks/taskTypes";
import { useActiveTaskRecovery } from "@/tasks/useActiveTaskRecovery";

import { DOCKER_TOAST_META } from "./dockerToast";
import {
  DockerUpdateOperationDialog,
  type DockerUpdateEvent,
  type DockerUpdateTarget,
} from "./DockerUpdateOperationDialog";
import { createDockerContainerUpdateRequest } from "./dockerUpdates";

interface DockerUpdateOperationContextValue {
  isUpdating: (containerId: string) => boolean;
  startUpdate: (containerId: string, containerName: string) => void;
  updating: boolean;
}

const DockerUpdateOperationContext =
  createContext<DockerUpdateOperationContextValue>({
    isUpdating: () => false,
    startUpdate: () => undefined,
    updating: false,
  });

export const useDockerUpdateOperation = () =>
  useContext(DockerUpdateOperationContext);

export function DockerUpdateOperationProvider({
  children,
}: {
  children: ReactNode;
}) {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const [target, setTarget] = useState<DockerUpdateTarget | null>(null);
  const [events, setEvents] = useState<DockerUpdateEvent[]>([]);
  const [result, setResult] = useState<DockerContainerUpdateResult | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const streamRef = useRef<Stream | null>(null);
  const { isOpen: muxIsOpen } = useStreamMux();

  const operation = linuxio.docker.update_container.useTaskStreamAction({
    closeMessage: "Container update stream closed unexpectedly",
    error: (streamError) => {
      if (streamError.name === "AbortError") return;
      streamRef.current = null;
      setError(streamError.message || "Container update failed");
    },
    onOpen: (stream) => {
      streamRef.current = stream;
    },
    onProgress: (progress: TaskProgress<DockerContainerUpdateProgress>) => {
      const detail = progress.detail;
      const phase = detail?.phase || progress.phase || "running";
      const message = detail?.message || progress.message;
      if (!message) return;
      setEvents((previous) => {
        const last = previous[previous.length - 1];
        if (last?.phase === phase && last.message === message) return previous;
        return [...previous, { message, phase }];
      });
    },
    openErrorMessage: "Failed to watch the container update",
    success: (updateResult) => {
      streamRef.current = null;
      setResult(updateResult);
      setError(null);
    },
  });
  const { isPending, mutate, watch } = operation;

  const begin = useCallback(
    (nextTarget: DockerUpdateTarget, request: DockerContainerUpdateRequest) => {
      setTarget(nextTarget);
      setEvents([]);
      setResult(null);
      setError(null);
      setOpen(true);
      mutate(request);
    },
    [mutate],
  );

  const startUpdate = useCallback(
    (containerId: string, containerName: string) => {
      if (isPending) {
        setOpen(true);
        return;
      }
      begin(
        { id: containerId, name: containerName },
        createDockerContainerUpdateRequest(containerId),
      );
    },
    [begin, isPending],
  );

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
      setEvents([]);
      setResult(null);
      setError(null);
      setOpen(true);
      watch(task, { containerId, runId: task.id });
    },
  });

  useEffect(
    () => () => {
      streamRef.current?.close();
    },
    [],
  );

  const handleClose = () => {
    setOpen(false);
    if (isPending) {
      toast.info("Container update is still running in the background");
    }
  };

  const value = useMemo<DockerUpdateOperationContextValue>(
    () => ({
      isUpdating: (containerId) => isPending && target?.id === containerId,
      startUpdate,
      updating: isPending,
    }),
    [isPending, startUpdate, target?.id],
  );

  return (
    <DockerUpdateOperationContext.Provider value={value}>
      {children}
      <DockerUpdateOperationDialog
        error={error}
        events={events}
        onClose={handleClose}
        open={open}
        result={result}
        running={isPending}
        target={target}
      />
    </DockerUpdateOperationContext.Provider>
  );
}
