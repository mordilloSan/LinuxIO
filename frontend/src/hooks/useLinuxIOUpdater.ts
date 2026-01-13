import { useContext } from "react";

import {
  UpdateContext,
  type UpdateContextValue,
} from "@/contexts/UpdateContext";

export type { UpdatePhase, UpdateContextValue } from "@/contexts/UpdateContext";

export const useLinuxIOUpdater = (): UpdateContextValue => {
  const context = useContext(UpdateContext);
  if (!context) {
    throw new Error("UpdateContext must be placed within UpdateProvider");
  }
  return context;
};
