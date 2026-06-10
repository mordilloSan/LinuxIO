import type { QueryKey } from "@tanstack/react-query";

import { linuxio } from "@/api";

// Query keys to invalidate when a job of the given type reaches a terminal state.
// Only listed types are auto-invalidated; jobs claimed by a local handler
// (registered via markJobLocallyHandled) are skipped.
export const INVALIDATIONS_BY_JOB_TYPE: Record<string, () => QueryKey[]> = {
  "docker.start_container": () => [linuxio.docker.list_containers.queryKey()],
  "docker.stop_container": () => [linuxio.docker.list_containers.queryKey()],
  "docker.restart_container": () => [linuxio.docker.list_containers.queryKey()],
  "docker.remove_container": () => [linuxio.docker.list_containers.queryKey()],
  "docker.start_all_stopped": () => [linuxio.docker.list_containers.queryKey()],
  "docker.stop_all_running": () => [linuxio.docker.list_containers.queryKey()],
  "docker.check_updates": () => [
    linuxio.docker.list_containers.queryKey(),
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_images.queryKey(),
  ],
  "docker.update_container": () => [
    linuxio.docker.list_containers.queryKey(),
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_images.queryKey(),
  ],
  "docker.delete_image": () => [linuxio.docker.list_images.queryKey()],

  "docker.create_network": () => [linuxio.docker.list_networks.queryKey()],
  "docker.delete_network": () => [linuxio.docker.list_networks.queryKey()],

  "docker.create_volume": () => [linuxio.docker.list_volumes.queryKey()],
  "docker.delete_volume": () => [linuxio.docker.list_volumes.queryKey()],

  "docker.compose_up": () => [
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_containers.queryKey(),
  ],
  "docker.compose_down": () => [
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_containers.queryKey(),
  ],
  "docker.compose_stop": () => [
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_containers.queryKey(),
  ],
  "docker.compose_restart": () => [
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_containers.queryKey(),
  ],
  "docker.delete_stack": () => [
    linuxio.docker.list_compose_projects.queryKey(),
    linuxio.docker.list_containers.queryKey(),
  ],

  "accounts.create_user": () => [linuxio.accounts.list_users.queryKey()],
  "accounts.delete_user": () => [linuxio.accounts.list_users.queryKey()],
  "accounts.modify_user": () => [linuxio.accounts.list_users.queryKey()],
  "accounts.lock_user": () => [linuxio.accounts.list_users.queryKey()],
  "accounts.unlock_user": () => [linuxio.accounts.list_users.queryKey()],
  "accounts.change_password": () => [linuxio.accounts.list_users.queryKey()],

  "accounts.create_group": () => [linuxio.accounts.list_groups.queryKey()],
  "accounts.delete_group": () => [linuxio.accounts.list_groups.queryKey()],
  "accounts.modify_group_members": () => [
    linuxio.accounts.list_groups.queryKey(),
    linuxio.accounts.list_users.queryKey(),
  ],
};
