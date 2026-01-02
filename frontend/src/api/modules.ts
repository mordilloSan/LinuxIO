import { linuxio } from "./linuxio";
import type { ModuleInfo } from "@/types/module";

/**
 * React Query hook to fetch loaded modules from backend
 */
export function useModules() {
  return linuxio.useCall<ModuleInfo[]>("modules", "GetModules", [], {
    staleTime: 60000, // Cache for 1 minute
    refetchOnMount: false,
  });
}
