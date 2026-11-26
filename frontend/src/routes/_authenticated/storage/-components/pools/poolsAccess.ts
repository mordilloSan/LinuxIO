import type { AccessPolicy } from "@/hooks/useCapabilities";

/**
 * Who may open the Pools tab and a pool's page; the tab strip, the route
 * guard and the redirect of an old pool link all read it.
 */
export const POOLS_ACCESS = {
  requiredCapabilities: ["btrfsAvailable"],
  requiresPrivileged: true,
} satisfies AccessPolicy;
