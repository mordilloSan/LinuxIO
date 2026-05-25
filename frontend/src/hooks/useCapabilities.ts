import { useMemo } from "react";

import {
  CAPABILITIES,
  pickCapabilityState,
  type CapabilityKey,
  type CapabilityState,
} from "@/api/capabilities";
import useAuth from "@/hooks/useAuth";

export type { CapabilityKey };
export type CapabilityStatus = "unknown" | "available" | "unavailable";

export type AccessContext = CapabilityState & {
  privileged: boolean;
};

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

const capabilityByKey = new Map(
  CAPABILITIES.map((c) => [c.state as CapabilityKey, c]),
);

export const getCapabilityReason = (
  capability: CapabilityKey,
  status: CapabilityStatus,
): string => {
  if (status === "available") return "";
  const def = capabilityByKey.get(capability);
  if (!def) return "";
  return status === "unknown" ? def.reasonUnknown : def.reasonUnavailable;
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
  const auth = useAuth();
  return useMemo(
    () => ({
      privileged: auth.privileged,
      ...pickCapabilityState(auth),
    }),
    [auth],
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
