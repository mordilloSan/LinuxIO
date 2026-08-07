import { createContext } from "react";

import type { AppConfig } from "@/api";
import type { ConfigContextType } from "@/types/config";

export const ConfigContext = createContext<ConfigContextType | undefined>(
  undefined,
);

export interface ConfigAccessorContextValue {
  /** Current config, read through a ref — always fresh, never a rerender. */
  getConfig: () => AppConfig;
}

// Identity-stable escape hatch for providers that must not rerender on
// config changes (BackgroundJobsProvider): its value never changes after
// mount, so subscribing costs nothing per config update.
export const ConfigAccessorContext =
  createContext<ConfigAccessorContextValue | null>(null);
