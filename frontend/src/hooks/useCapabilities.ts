import { useMemo } from "react";

import useAuth from "@/hooks/useAuth";

export type CapabilityKey =
  | "dockerAvailable"
  | "indexerAvailable"
  | "lmSensorsAvailable"
  | "smartmontoolsAvailable"
  | "packageKitAvailable"
  | "nfsAvailable"
  | "tunedAvailable";
export type CapabilityStatus = "unknown" | "available" | "unavailable";

export interface AccessContext {
  privileged: boolean;
  dockerAvailable: boolean | null;
  indexerAvailable: boolean | null;
  lmSensorsAvailable: boolean | null;
  smartmontoolsAvailable: boolean | null;
  packageKitAvailable: boolean | null;
  nfsAvailable: boolean | null;
  tunedAvailable: boolean | null;
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

  if (capability === "lmSensorsAvailable") {
    return status === "unknown"
      ? "lm-sensors dependency check is still running."
      : "lm-sensors dependency is unavailable.";
  }

  if (capability === "smartmontoolsAvailable") {
    return status === "unknown"
      ? "smartmontools dependency check is still running."
      : "smartmontools dependency is unavailable.";
  }

  if (capability === "packageKitAvailable") {
    return status === "unknown"
      ? "PackageKit availability is still being checked."
      : "PackageKit D-Bus service is unavailable.";
  }

  if (capability === "nfsAvailable") {
    return status === "unknown"
      ? "NFS utilities availability is still being checked."
      : "NFS utilities are unavailable.";
  }

  if (capability === "tunedAvailable") {
    return status === "unknown"
      ? "TuneD availability is still being checked."
      : "TuneD D-Bus service is unavailable.";
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
  const {
    privileged,
    dockerAvailable,
    indexerAvailable,
    lmSensorsAvailable,
    smartmontoolsAvailable,
    packageKitAvailable,
    nfsAvailable,
    tunedAvailable,
  } = useAuth();

  return useMemo(
    () => ({
      privileged,
      dockerAvailable,
      indexerAvailable,
      lmSensorsAvailable,
      smartmontoolsAvailable,
      packageKitAvailable,
      nfsAvailable,
      tunedAvailable,
    }),
    [
      privileged,
      dockerAvailable,
      indexerAvailable,
      lmSensorsAvailable,
      smartmontoolsAvailable,
      packageKitAvailable,
      nfsAvailable,
      tunedAvailable,
    ],
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
