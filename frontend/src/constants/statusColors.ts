export { SEMANTIC_STATUS_COLORS } from "@/theme/colors";
import { SEMANTIC_STATUS_COLORS } from "@/theme/colors";

/** Progress-bar colour for a fill percentage: >90 error, >70 warning. */
export const getUsageColor = (
  usedPct: number,
): "primary" | "warning" | "error" => {
  if (usedPct > 90) return "error";
  if (usedPct > 70) return "warning";
  return "primary";
};

export type ContainerTone = "success" | "warning" | "error";

/**
 * The one meaning of a container's state or health, for every Docker page, the
 * home dashboard card and the volume chips. Accepts Docker's `State` and
 * health words and the display states of `getContainerDisplayState`, in any
 * case.
 *
 * - success: running, healthy. Up and serving.
 * - warning: unhealthy, paused, restarting, starting, created, removing,
 *   partially running and anything unrecognised. Up or on its way, but not
 *   something to rely on yet.
 * - error: exited, stopped, dead. Not running, and not coming back by itself.
 */
export const getContainerTone = (state: string): ContainerTone => {
  switch (state.toLowerCase()) {
    case "healthy":
    case "running":
      return "success";
    case "stopped":
    case "exited":
    case "dead":
      return "error";
    default:
      return "warning";
  }
};

export const getContainerStatusColor = (state: string): string =>
  SEMANTIC_STATUS_COLORS[getContainerTone(state)];

export const getComposeStatusColor = (status: string): string => {
  switch (status) {
    case "running":
      return SEMANTIC_STATUS_COLORS.success;
    case "partial":
      return SEMANTIC_STATUS_COLORS.warning;
    case "stopped":
    default:
      return SEMANTIC_STATUS_COLORS.neutral;
  }
};

export const getWireguardStatusColor = (isConnected: string): string =>
  isConnected === "Active"
    ? SEMANTIC_STATUS_COLORS.success
    : SEMANTIC_STATUS_COLORS.neutral;

export const getServiceStatusColor = (activeState: string): string => {
  switch (activeState) {
    case "active":
      return SEMANTIC_STATUS_COLORS.success;
    case "failed":
      return SEMANTIC_STATUS_COLORS.danger;
    default:
      return SEMANTIC_STATUS_COLORS.neutral;
  }
};

export const getVMStatusColor = (state: string): string => {
  switch (state) {
    case "running":
      return SEMANTIC_STATUS_COLORS.success;
    case "paused":
    case "pmsuspended":
      return SEMANTIC_STATUS_COLORS.warning;
    case "crashed":
      return SEMANTIC_STATUS_COLORS.danger;
    default:
      return SEMANTIC_STATUS_COLORS.neutral;
  }
};
