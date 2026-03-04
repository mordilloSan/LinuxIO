import { useMemo } from "react";

import useAuth from "@/hooks/useAuth";

export type CapabilityKey = "dockerAvailable" | "indexerAvailable";
export type CapabilityStatus = "unknown" | "available" | "unavailable";

export interface AccessContext {
  privileged: boolean;
  dockerAvailable: boolean | null;
  indexerAvailable: boolean | null;
}

export interface AccessPolicy {
  requiresPrivileged?: boolean;
  requiredCapabilities?: CapabilityKey[];
}

export const getCapabilityStatus = (
  capability: boolean | null | undefined,
): CapabilityStatus => {
  if (capability === true) return "available";
  if (capability === false) return "unavailable";
  return "unknown";
};

export const isCapabilityEnabled = (
  capability: boolean | null | undefined,
): boolean => capability === true;

export const getCapabilityReason = (
  capability: CapabilityKey,
  status: CapabilityStatus,
): string => {
  if (status === "available") return "";

  if (capability === "indexerAvailable") {
    return status === "unknown"
      ? "Indexer availability is still being checked."
      : "Indexer service is unavailable.";
  }

  return status === "unknown"
    ? "Docker availability is still being checked."
    : "Docker service is unavailable.";
};

export const hasAccessPolicy = (
  policy: AccessPolicy | undefined,
  access: AccessContext,
): boolean => {
  if (!policy) return true;
  if (policy.requiresPrivileged && !access.privileged) return false;

  if (policy.requiredCapabilities && policy.requiredCapabilities.length > 0) {
    return policy.requiredCapabilities.every((capability) =>
      isCapabilityEnabled(access[capability]),
    );
  }

  return true;
};

export const useAccessContext = (): AccessContext => {
  const { privileged, dockerAvailable, indexerAvailable } = useAuth();

  return useMemo(
    () => ({
      privileged,
      dockerAvailable,
      indexerAvailable,
    }),
    [privileged, dockerAvailable, indexerAvailable],
  );
};

export const useCapability = (capability: CapabilityKey) => {
  const access = useAccessContext();
  const value = access[capability];
  const status = getCapabilityStatus(value);

  return useMemo(
    () => ({
      value,
      status,
      isEnabled: status === "available",
      reason: getCapabilityReason(capability, status),
    }),
    [capability, value, status],
  );
};
