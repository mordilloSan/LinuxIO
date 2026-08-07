import { createContext } from "react";

export interface PowerActionContextType {
  triggerPowerOff: () => void;
  triggerReboot: () => void;
}

export const PowerActionContext = createContext<
  PowerActionContextType | undefined
>(undefined);
