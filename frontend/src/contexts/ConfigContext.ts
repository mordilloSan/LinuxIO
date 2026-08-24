import { createContext } from "react";

import type { ConfigContextType } from "@/types/config";

export const ConfigContext = createContext<ConfigContextType | undefined>(
  undefined,
);
