export { SEMANTIC_STATUS_COLORS } from "@/theme/colors";
import { SEMANTIC_STATUS_COLORS } from "@/theme/colors";

export const getContainerStatusColor = (state: string): string => {
  if (state === "Healthy" || state === "Running") {
    return SEMANTIC_STATUS_COLORS.success;
  }
  if (state === "Unhealthy") {
    return SEMANTIC_STATUS_COLORS.warning;
  }
  if (state === "Stopped" || state === "Dead") {
    return SEMANTIC_STATUS_COLORS.error;
  }
  return SEMANTIC_STATUS_COLORS.warning;
};

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

export const getLogPriorityAccent = (priorityColor: string): string => {
  switch (priorityColor) {
    case "error":
      return SEMANTIC_STATUS_COLORS.danger;
    case "warning":
      return SEMANTIC_STATUS_COLORS.caution;
    case "info":
      return SEMANTIC_STATUS_COLORS.info;
    case "success":
      return SEMANTIC_STATUS_COLORS.success;
    default:
      return SEMANTIC_STATUS_COLORS.muted;
  }
};
