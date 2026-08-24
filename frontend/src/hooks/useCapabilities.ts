import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import {
  CAPABILITIES,
  type CapabilitiesResponse,
  type CapabilityKey,
  type CapabilityState,
  capabilitiesQueryKey,
  capabilityStateFromWire,
  emptyCapabilityState,
} from "@/api/capabilities";
import useAuth from "@/hooks/useAuth";
import { useConfigUserId } from "@/hooks/useConfig";

export type CapabilityStatus = "unknown" | "available" | "unavailable";

export type AccessContext = CapabilityState & {
  privileged: boolean;
};

export interface AccessPolicy {
  requiredCapabilities?: CapabilityKey[];
  requiresPrivileged?: boolean;
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

const capabilityByKey = new Map(CAPABILITIES.map((c) => [c.state, c]));

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

/**
 * Latest capability scan from the per-user query cache; every flag is null
 * until a scan (or the stored bootstrap seed) lands. AuthProvider owns all
 * fetching — this hook only subscribes.
 */
export const useCapabilityState = (): CapabilityState => {
  const userId = useConfigUserId();
  const { data } = useQuery<
    Partial<CapabilitiesResponse>,
    Error,
    CapabilityState
  >({
    queryKey: capabilitiesQueryKey(userId),
    enabled: false,
    select: capabilityStateFromWire,
  });
  return data ?? emptyCapabilityState;
};

export const useAccessContext = (): AccessContext => {
  const { privileged } = useAuth();
  const capabilities = useCapabilityState();
  return useMemo(
    () => ({
      privileged,
      ...capabilities,
    }),
    [privileged, capabilities],
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
