import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

import {
  CACHE_TTL_MS,
  linuxio,
  type DockerContainerAutoUpdateOptions,
} from "@/api";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

const DOCKER_TOAST_META = { href: "/docker", label: "Open Docker" };
const DEFAULT_OPTIONS: DockerContainerAutoUpdateOptions = {
  cleanup: false,
  container_names: [],
  enabled: false,
  mode: "update",
  time: "04:00",
};

const uniqueNames = (names: string[]) => [...new Set(names)].sort();

export const useContainerAutoUpdateControls = () => {
  const queryClient = useQueryClient();
  const toast = useScopedToast(DOCKER_TOAST_META);
  const query = linuxio.docker.get_container_auto_update.useQuery({
    staleTime: CACHE_TTL_MS.TWO_SECONDS,
  });
  const mutation = linuxio.docker.set_container_auto_update.useMutation({
    onSuccess: (state) => {
      queryClient.setQueryData(
        linuxio.docker.get_container_auto_update.queryKey(),
        state,
      );
      queryClient.invalidateQueries({
        queryKey: linuxio.docker.get_container_auto_update.queryKey(),
      });
    },
    onError: (err: Error) =>
      toast.error(
        getMutationErrorMessage(
          err,
          "Failed to save container auto-update setting",
        ),
      ),
  });

  const containerNames =
    query.data?.options?.container_names ?? DEFAULT_OPTIONS.container_names;
  const selectedNames = useMemo(
    () => new Set(containerNames),
    [containerNames],
  );
  const disabled =
    query.isPending || mutation.isPending || !query.data?.available;
  const reason =
    query.error?.message ??
    query.data?.error ??
    (!query.data?.available ? "Watchtower is unavailable." : undefined);

  const toggleContainer = useCallback(
    (name: string) => {
      const state = query.data;
      if (!state) return;
      const options = state.options ?? DEFAULT_OPTIONS;

      const nextNames = new Set(options.container_names ?? []);
      const enabling = !nextNames.has(name);
      if (enabling) {
        nextNames.add(name);
      } else {
        nextNames.delete(name);
      }

      mutation.mutate(
        {
          ...options,
          container_names: uniqueNames(Array.from(nextNames)),
        },
        {
          onSuccess: () =>
            toast.success(
              enabling
                ? `Scheduled auto-update enabled for ${name}`
                : `Scheduled auto-update disabled for ${name}`,
            ),
        },
      );
    },
    [mutation, query.data, toast],
  );

  return {
    disabled,
    reason,
    selectedNames,
    toggleContainer,
  };
};
