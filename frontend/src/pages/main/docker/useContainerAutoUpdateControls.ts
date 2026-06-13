import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  CACHE_TTL_MS,
  linuxio,
  type DockerContainerAutoUpdateOptions,
  type DockerContainerAutoUpdateState,
} from "@/api";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

const DOCKER_TOAST_META = { href: "/docker", label: "Open Docker" };
const SAVE_DEBOUNCE_MS = 250;
const DEFAULT_OPTIONS: DockerContainerAutoUpdateOptions = {
  cleanup: false,
  container_names: [],
  enabled: false,
  mode: "update",
  time: "04:00",
};

const uniqueNames = (names: string[]) => [...new Set(names)].sort();

const normalizeOptions = (options: DockerContainerAutoUpdateOptions) => ({
  ...options,
  container_names: uniqueNames(options.container_names ?? []),
});

const optionsKey = (options: DockerContainerAutoUpdateOptions) =>
  JSON.stringify(normalizeOptions(options));

const diffNames = (confirmedNames: string[], desiredNames: string[]) => {
  const confirmed = new Set(confirmedNames);
  const desired = new Set(desiredNames);
  const pending = new Set<string>();

  for (const name of desired) {
    if (!confirmed.has(name)) pending.add(name);
  }
  for (const name of confirmed) {
    if (!desired.has(name)) pending.add(name);
  }

  return pending;
};

const stateWithOptions = (
  state: DockerContainerAutoUpdateState,
  options: DockerContainerAutoUpdateOptions,
): DockerContainerAutoUpdateState => {
  const selected = new Set(options.container_names ?? []);
  return {
    ...state,
    containers: (state.containers ?? []).map((container) => ({
      ...container,
      selected: selected.has(container.name),
    })),
    missing_container_names: (state.missing_container_names ?? []).filter(
      (name) => selected.has(name),
    ),
    options,
  };
};

export const useContainerAutoUpdateControls = () => {
  const queryClient = useQueryClient();
  const toast = useScopedToast(DOCKER_TOAST_META);
  const queryKey = linuxio.docker.get_container_auto_update.queryKey();
  const [confirmedOptions, setConfirmedOptions] =
    useState<DockerContainerAutoUpdateOptions | null>(null);
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
  const flushTimerRef = useRef<number | undefined>(undefined);
  const query = linuxio.docker.get_container_auto_update.useQuery({
    staleTime: CACHE_TTL_MS.TWO_SECONDS,
  });
  const { mutateAsync: saveAutoUpdateOptions } =
    linuxio.docker.set_container_auto_update.useMutation();

  const containerNames =
    query.data?.options?.container_names ?? DEFAULT_OPTIONS.container_names;
  const selectedNames = useMemo(
    () => new Set(containerNames),
    [containerNames],
  );
  const pendingNames = useMemo(
    () =>
      diffNames(
        confirmedOptions?.container_names ?? containerNames,
        containerNames,
      ),
    [confirmedOptions?.container_names, containerNames],
  );
  const disabled = query.isPending || !query.data?.available;
  const reason =
    query.error?.message ??
    query.data?.error ??
    (!query.data?.available ? "Watchtower is unavailable." : undefined);

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
        queryKey,
        stateWithOptions(query.data, desiredOptions),
      );
    }
  }, [query.data, queryClient, queryKey]);

  useEffect(
    () => () => {
      if (flushTimerRef.current !== undefined) {
        window.clearTimeout(flushTimerRef.current);
      }
    },
    [],
  );

  const runQueuedSave = useCallback(async () => {
    if (saveLoopRunningRef.current) return;

    if (flushTimerRef.current !== undefined) {
      window.clearTimeout(flushTimerRef.current);
      flushTimerRef.current = undefined;
    }

    saveLoopRunningRef.current = true;
    try {
      while (queuedOptionsRef.current) {
        const options = queuedOptionsRef.current;
        queuedOptionsRef.current = null;

        try {
          const savedState = await saveAutoUpdateOptions(options);
          const savedOptions = normalizeOptions(savedState.options);
          const desiredOptions = desiredOptionsRef.current ?? savedOptions;

          confirmedOptionsRef.current = savedOptions;
          setConfirmedOptions(savedOptions);
          queryClient.setQueryData(
            queryKey,
            optionsKey(savedOptions) === optionsKey(desiredOptions)
              ? savedState
              : stateWithOptions(savedState, desiredOptions),
          );

          if (optionsKey(savedOptions) === optionsKey(desiredOptions)) {
            toast.success("Container auto-update settings saved");
          }
        } catch (err) {
          const desiredOptions = desiredOptionsRef.current;
          if (
            desiredOptions &&
            optionsKey(desiredOptions) !== optionsKey(options)
          ) {
            queuedOptionsRef.current = desiredOptions;
            continue;
          }

          const confirmed = confirmedOptionsRef.current ?? DEFAULT_OPTIONS;
          desiredOptionsRef.current = confirmed;
          const current =
            queryClient.getQueryData<DockerContainerAutoUpdateState>(queryKey);
          if (current) {
            queryClient.setQueryData(
              queryKey,
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
    } finally {
      saveLoopRunningRef.current = false;
    }
  }, [queryClient, queryKey, saveAutoUpdateOptions, toast]);

  const scheduleSave = useCallback(
    (options: DockerContainerAutoUpdateOptions) => {
      queuedOptionsRef.current = normalizeOptions(options);
      if (saveLoopRunningRef.current) return;

      if (flushTimerRef.current !== undefined) {
        window.clearTimeout(flushTimerRef.current);
      }
      flushTimerRef.current = window.setTimeout(() => {
        flushTimerRef.current = undefined;
        void runQueuedSave();
      }, SAVE_DEBOUNCE_MS);
    },
    [runQueuedSave],
  );

  const toggleContainer = useCallback(
    (name: string) => {
      const state =
        queryClient.getQueryData<DockerContainerAutoUpdateState>(queryKey) ??
        query.data;
      if (!state) return;
      const options = state.options ?? DEFAULT_OPTIONS;

      const nextNames = new Set(options.container_names ?? []);
      const enabling = !nextNames.has(name);
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
      void queryClient.cancelQueries({ queryKey });
      queryClient.setQueryData(queryKey, stateWithOptions(state, nextOptions));
      scheduleSave(nextOptions);
    },
    [query.data, queryClient, queryKey, scheduleSave],
  );

  return {
    disabled,
    pendingNames,
    reason,
    selectedNames,
    toggleContainer,
  };
};
