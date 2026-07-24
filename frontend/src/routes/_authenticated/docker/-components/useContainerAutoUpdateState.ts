import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import {
  CACHE_TTL_MS,
  linuxio,
  type DockerContainerAutoUpdateOptions,
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
  const autoUpdateCache = linuxio.docker.get_container_auto_update.useCache();
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
  const flushTimerRef = useRef<number | undefined>(undefined);
  const query = useQuery(
    linuxio.docker.get_container_auto_update.queryOptions({
      staleTime: CACHE_TTL_MS.TWO_SECONDS,
    }),
  );
  // This hook reconciles the cache itself (optimistic setQueryData + save
  // loop), so opt out of the manifest invalidation.
  const { mutateAsync: saveAutoUpdateOptions } =
    linuxio.docker.set_container_auto_update.useJobAction({ invalidates: [] });

  const containerNames =
    query.data?.options?.container_names ??
    DEFAULT_AUTO_UPDATE_OPTIONS.container_names;
  const selectedNames = new Set(containerNames);
  const pendingNames = diffNames(
    confirmedOptions?.container_names ?? containerNames,
    containerNames,
  );
  const disabled = query.isPending || !query.data?.available;
  const reason = query.isPending
    ? "Loading scheduled auto-update settings..."
    : (query.error?.message ??
      query.data?.error ??
      (!query.data?.available ? "Watchtower is unavailable." : undefined));

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
      autoUpdateCache.set(stateWithOptions(query.data, desiredOptions));
    }
  }, [autoUpdateCache, query.data]);

  useEffect(
    () => () => {
      if (flushTimerRef.current !== undefined) {
        window.clearTimeout(flushTimerRef.current);
      }
    },
    [],
  );

  const runQueuedSave = async () => {
    if (saveLoopRunningRef.current) return;

    if (flushTimerRef.current !== undefined) {
      window.clearTimeout(flushTimerRef.current);
      flushTimerRef.current = undefined;
    }

    saveLoopRunningRef.current = true;
    setIsSaving(true);
    // No try/finally around the loop — the React Compiler (oxc port) cannot
    // lower finalizers yet. Each iteration catches its own errors without
    // rethrowing, so the flag reset below is always reached.
    while (queuedOptionsRef.current) {
      const options = queuedOptionsRef.current;
      queuedOptionsRef.current = null;

      try {
        const savedState = await saveAutoUpdateOptions(options);
        const savedOptions = normalizeOptions(savedState.options);
        const desiredOptions = desiredOptionsRef.current ?? savedOptions;

        confirmedOptionsRef.current = savedOptions;
        setConfirmedOptions(savedOptions);
        autoUpdateCache.set(
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

        const confirmed =
          confirmedOptionsRef.current ?? DEFAULT_AUTO_UPDATE_OPTIONS;
        desiredOptionsRef.current = confirmed;
        const current = autoUpdateCache.get();
        if (current) {
          autoUpdateCache.set(stateWithOptions(current, confirmed));
        }
        toast.error(
          getMutationErrorMessage(
            err,
            "Failed to save container auto-update setting",
          ),
        );
      }
    }
    saveLoopRunningRef.current = false;
    setIsSaving(false);
  };

  const scheduleSave = (options: DockerContainerAutoUpdateOptions) => {
    queuedOptionsRef.current = normalizeOptions(options);
    if (saveLoopRunningRef.current) return;

    if (flushTimerRef.current !== undefined) {
      window.clearTimeout(flushTimerRef.current);
    }
    flushTimerRef.current = window.setTimeout(() => {
      flushTimerRef.current = undefined;
      void runQueuedSave();
    }, SAVE_DEBOUNCE_MS);
  };

  const toggleContainer = (name: string) => {
    const state = autoUpdateCache.get();
    if (!state) return;
    const options = state.options ?? DEFAULT_AUTO_UPDATE_OPTIONS;

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
    void autoUpdateCache.cancel();
    autoUpdateCache.set(stateWithOptions(state, nextOptions));
    scheduleSave(nextOptions);
  };

  // Explicit whole-form save (settings dialog): optimistic like the toggles,
  // but flushed immediately instead of waiting out the toggle debounce.
  const saveOptions = (options: DockerContainerAutoUpdateOptions) => {
    const nextOptions = normalizeOptions(options);
    desiredOptionsRef.current = nextOptions;
    const current = autoUpdateCache.get();
    if (current) {
      void autoUpdateCache.cancel();
      autoUpdateCache.set(stateWithOptions(current, nextOptions));
    }
    queuedOptionsRef.current = nextOptions;
    void runQueuedSave();
  };

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
    toggleContainer,
  };
};
