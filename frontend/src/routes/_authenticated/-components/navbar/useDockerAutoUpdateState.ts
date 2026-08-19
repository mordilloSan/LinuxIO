import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  linuxio,
  useCallMutation,
  type DockerContainerAutoUpdateOptions,
  type DockerContainerAutoUpdateState,
} from "@/api";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

import {
  DEFAULT_AUTO_UPDATE_OPTIONS,
  normalizeOptions,
  optionsKey,
  stateWithOptions,
} from "./dockerAutoUpdateState";

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

export interface ContainerAutoUpdateTargetEligibility {
  mutationAllowed: boolean;
  mutationReason?: string;
}

export type DockerAutoUpdateController = ReturnType<
  typeof useDockerAutoUpdateState
>;

/**
 * Settings-owned writer for scheduled Docker update policy. Consecutive saves
 * are coalesced while preserving optimistic cache state and rollback behavior.
 */
export const useDockerAutoUpdateState = () => {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const queryClient = useQueryClient();
  const autoUpdateKey = linuxio.docker.get_container_auto_update.queryKey;
  const [isSaving, setIsSaving] = useState(false);
  const confirmedOptionsRef = useRef<DockerContainerAutoUpdateOptions | null>(
    null,
  );
  const desiredOptionsRef = useRef<DockerContainerAutoUpdateOptions | null>(
    null,
  );
  const queuedOptionsRef = useRef<DockerContainerAutoUpdateOptions | null>(
    null,
  );
  const saveLoopRunningRef = useRef(false);
  const mountedRef = useRef(true);
  const query = useQuery({
    ...linuxio.docker.get_container_auto_update,
    staleTime: 2000,
  });
  // This hook reconciles the cache itself (optimistic setQueryData + save
  // loop), so opt out of the manifest invalidation.
  const { mutateAsync: saveAutoUpdateOptions } = useCallMutation(
    linuxio.docker.set_container_auto_update,
    { invalidates: [] },
  );

  const targetEligibility = useMemo<
    ReadonlyMap<string, ContainerAutoUpdateTargetEligibility>
  >(
    () =>
      new Map(
        (query.data?.containers ?? []).map(
          (target) =>
            [
              target.name,
              {
                mutationAllowed: target.mutationAllowed,
                mutationReason: target.mutationReason,
              },
            ] as const,
        ),
      ),
    [query.data?.containers],
  );
  useEffect(() => {
    if (!query.data?.options) return;
    if (saveLoopRunningRef.current || queuedOptionsRef.current) return;

    const options = normalizeOptions(query.data.options);
    confirmedOptionsRef.current = options;
    desiredOptionsRef.current = options;
  }, [query.data?.options]);

  useEffect(() => {
    if (!query.data) return;
    if (!saveLoopRunningRef.current && !queuedOptionsRef.current) return;

    const desiredOptions = desiredOptionsRef.current;
    if (
      desiredOptions &&
      optionsKey(query.data.options) !== optionsKey(desiredOptions)
    ) {
      queryClient.setQueryData(
        autoUpdateKey,
        stateWithOptions(query.data, desiredOptions),
      );
    }
  }, [autoUpdateKey, query.data, queryClient]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      queuedOptionsRef.current = null;
    };
  }, []);

  const runQueuedSave = useCallback(() => {
    if (!mountedRef.current) return;
    if (saveLoopRunningRef.current) return;

    saveLoopRunningRef.current = true;
    setIsSaving(true);
    const drainQueuedSaves = async () => {
      while (mountedRef.current && queuedOptionsRef.current) {
        const options = queuedOptionsRef.current;
        queuedOptionsRef.current = null;

        try {
          const savedState = await saveAutoUpdateOptions(options);
          if (!mountedRef.current) break;
          const savedOptions = normalizeOptions(savedState.options);
          const desiredOptions = desiredOptionsRef.current ?? savedOptions;

          confirmedOptionsRef.current = savedOptions;
          queryClient.setQueryData(
            autoUpdateKey,
            optionsKey(savedOptions) === optionsKey(desiredOptions)
              ? savedState
              : stateWithOptions(savedState, desiredOptions),
          );

          if (optionsKey(savedOptions) === optionsKey(desiredOptions)) {
            toast.success("Scheduled update settings saved");
          }
        } catch (err) {
          if (!mountedRef.current) break;
          const desiredOptions = desiredOptionsRef.current;
          if (
            desiredOptions &&
            optionsKey(desiredOptions) !== optionsKey(options)
          ) {
            queuedOptionsRef.current = desiredOptions;
            continue;
          }

          const confirmed =
            confirmedOptionsRef.current ?? DEFAULT_AUTO_UPDATE_OPTIONS;
          desiredOptionsRef.current = confirmed;
          const current =
            queryClient.getQueryData<DockerContainerAutoUpdateState>(
              autoUpdateKey,
            );
          if (current) {
            queryClient.setQueryData(
              autoUpdateKey,
              stateWithOptions(current, confirmed),
            );
          }
          toast.error(
            getMutationErrorMessage(
              err,
              "Failed to save scheduled update settings",
            ),
          );
        }
      }
    };

    // Promise.finally provides unconditional cleanup without the
    // try/finally syntax the current React Compiler cannot lower.
    void drainQueuedSaves().finally(() => {
      saveLoopRunningRef.current = false;
      if (mountedRef.current) setIsSaving(false);
    });
  }, [autoUpdateKey, queryClient, saveAutoUpdateOptions, toast]);

  // Explicit whole-form save updates the cache optimistically and flushes the
  // latest desired policy through the single settings-owned save loop.
  const saveOptions = useCallback(
    (options: DockerContainerAutoUpdateOptions) => {
      if (!mountedRef.current) return;
      const nextOptions = normalizeOptions(options);
      desiredOptionsRef.current = nextOptions;
      const current =
        queryClient.getQueryData<DockerContainerAutoUpdateState>(autoUpdateKey);
      if (current) {
        void queryClient.cancelQueries({ queryKey: autoUpdateKey });
        queryClient.setQueryData(
          autoUpdateKey,
          stateWithOptions(current, nextOptions),
        );
      }
      queuedOptionsRef.current = nextOptions;
      runQueuedSave();
    },
    [autoUpdateKey, queryClient, runQueuedSave],
  );

  return {
    isPending: query.isPending,
    isSaving,
    queryError: query.error?.message,
    saveOptions,
    state: query.data,
    targetEligibility,
  };
};
