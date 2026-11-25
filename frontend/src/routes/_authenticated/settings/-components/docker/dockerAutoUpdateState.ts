import type {
  DockerContainerAutoUpdateOptions,
  DockerContainerAutoUpdateState,
} from "@/api";

export const DEFAULT_AUTO_UPDATE_OPTIONS: DockerContainerAutoUpdateOptions = {
  cleanup: false,
  container_names: [],
  enabled: false,
  include_stopped: false,
  min_free_gib: 0,
  mode: "update",
  revive_stopped: false,
  time: "04:00",
  update_all: false,
  update_stopped: false,
};

/** The backend's bound on `min_free_gib` (`maxMinFreeGiB`), 1 PiB. */
export const MAX_MIN_FREE_GIB = 1 << 20;

const uniqueNames = (names: string[]) => [...new Set(names)].sort();

export const normalizeOptions = (
  options: DockerContainerAutoUpdateOptions,
): DockerContainerAutoUpdateOptions => ({
  ...options,
  container_names: uniqueNames(options.container_names ?? []),
});

/** Canonical comparison key: normalized options, order-insensitive names. */
export const optionsKey = (options: DockerContainerAutoUpdateOptions) =>
  JSON.stringify(normalizeOptions(options));

/** Re-derive the per-container `selected` flags for an options override. */
export const stateWithOptions = (
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
