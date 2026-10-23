import { createContext } from "react";

import type { ConfigContextType } from "@/api";

export const ConfigContext = createContext<ConfigContextType | undefined>(
  undefined,
);
