import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useEffect } from "react";

import {
  bindStreamHandlers,
  isJobLocallyHandled,
  isTerminalJobState,
  linuxio,
  openJobEventsStream,
  useStreamMux,
  type JobEvent,
  type Stream,
} from "@/api";

const INVALIDATIONS_BY_JOB_TYPE: Record<string, QueryKey[]> = {
  "docker.start_container": [linuxio.docker.list_containers.queryKey()],
  "docker.stop_container": [linuxio.docker.list_containers.queryKey()],
  "docker.restart_container": [linuxio.docker.list_containers.queryKey()],
  "docker.remove_container": [linuxio.docker.list_containers.queryKey()],
  "docker.start_all_stopped": [linuxio.docker.list_containers.queryKey()],
  "docker.stop_all_running": [linuxio.docker.list_containers.queryKey()],

  "docker.delete_image": [linuxio.docker.list_images.queryKey()],

  "docker.create_network": [linuxio.docker.list_networks.queryKey()],
  "docker.delete_network": [linuxio.docker.list_networks.queryKey()],

  "docker.create_volume": [linuxio.docker.list_volumes.queryKey()],
  "docker.delete_volume": [linuxio.docker.list_volumes.queryKey()],

  "docker.compose_up": [
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_containers.queryKey(),
  ],
  "docker.compose_down": [
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_containers.queryKey(),
  ],
  "docker.compose_stop": [
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_containers.queryKey(),
  ],
  "docker.compose_restart": [
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_containers.queryKey(),
  ],
  "docker.delete_stack": [
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_containers.queryKey(),
  ],

  "accounts.create_user": [linuxio.accounts.list_users.queryKey()],
  "accounts.delete_user": [linuxio.accounts.list_users.queryKey()],
  "accounts.modify_user": [linuxio.accounts.list_users.queryKey()],
  "accounts.lock_user": [linuxio.accounts.list_users.queryKey()],
  "accounts.unlock_user": [linuxio.accounts.list_users.queryKey()],
  "accounts.change_password": [linuxio.accounts.list_users.queryKey()],

  "accounts.create_group": [linuxio.accounts.list_groups.queryKey()],
  "accounts.delete_group": [linuxio.accounts.list_groups.queryKey()],
  "accounts.modify_group_members": [
    linuxio.accounts.list_groups.queryKey(),
    linuxio.accounts.list_users.queryKey(),
  ],
};

export const JobsEventsListener: React.FC = () => {
  const queryClient = useQueryClient();
  const { status: streamMuxStatus } = useStreamMux();

  useEffect(() => {
    if (streamMuxStatus !== "open") return;

    let cancelled = false;
    let cleanup: (() => void) | undefined;
    let eventStream: Stream | null = null;

    eventStream = openJobEventsStream();
    if (!eventStream) return;

    cleanup = bindStreamHandlers<JobEvent>(eventStream, {
      onProgress: (event) => {
        if (cancelled || !event?.job) return;
        if (!isTerminalJobState(event.job.state)) return;
        if (isJobLocallyHandled(event.job.id)) return;

        const queryKeys = INVALIDATIONS_BY_JOB_TYPE[event.job.type];
        if (!queryKeys) return;
        for (const queryKey of queryKeys) {
          void queryClient.invalidateQueries({ queryKey });
        }
      },
    });

    return () => {
      cancelled = true;
      cleanup?.();
      eventStream?.close();
    };
  }, [queryClient, streamMuxStatus]);

  return null;
};
