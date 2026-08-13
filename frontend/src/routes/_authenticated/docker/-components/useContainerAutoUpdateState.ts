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
  diffNames,
  normalizeOptions,
  optionsKey,
  stateWithOptions,
} from "./containerAutoUpdate";

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;
const SAVE_DEBOUNCE_MS = 250;

export interface ContainerAutoUpdateTargetEligibility {
  mutationAllowed: boolean;
  mutationReason?: string;
}

export const canEnableContainerAutoUpdateTarget = (
  mode: DockerContainerAutoUpdateOptions["mode"],
  eligibility?: ContainerAutoUpdateTargetEligibility,
) => mode === "check_only" || eligibility?.mutationAllowed !== false;

export type ContainerAutoUpdateController = ReturnType<
  typeof useContainerAutoUpdateState
>;

/**
 * The single writer for `docker.get_container_auto_update` state. Every save
 * — the container list's optimistic per-container toggles and the settings
 * dialog's explicit Save — funnels through one coalescing queue, so the two
 * surfaces cannot clobber each other's writes. Instantiate once per page and
 * pass the controller down.
 */
export const useContainerAutoUpdateState = () => {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const queryClient = useQueryClient();
  const autoUpdateKey = linuxio.docker.get_container_auto_update.queryKey;
  const [confirmedOptions, setConfirmedOptions] =
    useState<DockerContainerAutoUpdateOptions | null>(null);
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
  const flushTimerRef = useRef<number | undefined>(undefined);
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

  const containerNames =
    query.data?.options?.container_names ??
    DEFAULT_AUTO_UPDATE_OPTIONS.container_names;
  // Both sets are consumed as memo dependencies by the table's column builder.
  // A fresh identity per render rebuilds the columns, and rebuilt columns
  // remount every cell — including an action button with a click already in
  // flight over it, which swallows the click outright.
  const selectedNames = useMemo(
    () => new Set(containerNames),
    [containerNames],
  );
  const confirmedNames = confirmedOptions?.container_names;
  const pendingNames = useMemo(
    () => diffNames(confirmedNames ?? containerNames, containerNames),
    [confirmedNames, containerNames],
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
  const disabled = query.isPending || !query.data?.available;
  const reason = query.isPending
    ? "Loading scheduled auto-update settings..."
    : (query.error?.message ??
      query.data?.error ??
      (!query.data?.available ? "Docker updates are unavailable." : undefined));

  useEffect(() => {
    if (!query.data?.options) return;
    if (saveLoopRunningRef.current || queuedOptionsRef.current) return;

    const options = normalizeOptions(query.data.options);
    confirmedOptionsRef.current = options;
    desiredOptionsRef.current = options;
    setConfirmedOptions(options);
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
      if (flushTimerRef.current !== undefined) {
        window.clearTimeout(flushTimerRef.current);
        flushTimerRef.current = undefined;
      }
      queuedOptionsRef.current = null;
    };
  }, []);

  const runQueuedSave = useCallback(() => {
    if (!mountedRef.current) return;
    if (saveLoopRunningRef.current) return;

    if (flushTimerRef.current !== undefined) {
      window.clearTimeout(flushTimerRef.current);
      flushTimerRef.current = undefined;
    }

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
          setConfirmedOptions(savedOptions);
          queryClient.setQueryData(
            autoUpdateKey,
            optionsKey(savedOptions) === optionsKey(desiredOptions)
              ? savedState
              : stateWithOptions(savedState, desiredOptions),
          );

          if (optionsKey(savedOptions) === optionsKey(desiredOptions)) {
            toast.success("Container auto-update settings saved");
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
              "Failed to save container auto-update setting",
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

  const scheduleSave = useCallback(
    (options: DockerContainerAutoUpdateOptions) => {
      if (!mountedRef.current) return;
      queuedOptionsRef.current = normalizeOptions(options);
      if (saveLoopRunningRef.current) return;

      if (flushTimerRef.current !== undefined) {
        window.clearTimeout(flushTimerRef.current);
      }
      flushTimerRef.current = window.setTimeout(() => {
        flushTimerRef.current = undefined;
        runQueuedSave();
      }, SAVE_DEBOUNCE_MS);
    },
    [runQueuedSave],
  );

  const toggleContainer = useCallback(
    (name: string) => {
      if (!mountedRef.current) return;
      const state =
        queryClient.getQueryData<DockerContainerAutoUpdateState>(autoUpdateKey);
      if (!state) return;
      const options = state.options ?? DEFAULT_AUTO_UPDATE_OPTIONS;

      const nextNames = new Set(options.container_names ?? []);
      const enabling = !nextNames.has(name);
      if (
        enabling &&
        !canEnableContainerAutoUpdateTarget(
          options.mode,
          targetEligibility.get(name),
        )
      ) {
        return;
      }
      if (enabling) {
        nextNames.add(name);
      } else {
        nextNames.delete(name);
      }
      const nextOptions = normalizeOptions({
        ...options,
        container_names: Array.from(nextNames),
      });

      desiredOptionsRef.current = nextOptions;
      void queryClient.cancelQueries({ queryKey: autoUpdateKey });
      queryClient.setQueryData(
        autoUpdateKey,
        stateWithOptions(state, nextOptions),
      );
      scheduleSave(nextOptions);
    },
    [autoUpdateKey, queryClient, scheduleSave, targetEligibility],
  );

  // Explicit whole-form save (settings dialog): optimistic like the toggles,
  // but flushed immediately instead of waiting out the toggle debounce.
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
    disabled,
    isPending: query.isPending,
    isSaving,
    pendingNames,
    queryError: query.error?.message,
    reason,
    saveOptions,
    selectedNames,
    state: query.data,
    targetEligibility,
    toggleContainer,
  };
};
