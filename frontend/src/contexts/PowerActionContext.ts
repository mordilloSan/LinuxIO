import { createContext } from "react";

/**
 * Reports how one power request answered: no argument for success, the
 * mutation's error otherwise. A request that did not run (failed on the host, or never
 * sent) takes the overlay down and shows the reason; a lost connection or any
 * other unknown outcome leaves it up. Ignored once its request is no longer
 * the current one.
 */
export type SettlePowerAction = (error?: unknown) => void;

export interface PowerActionContextType {
  /** Shows the overlay; the caller settles it with the returned function. */
  triggerPowerOff: () => SettlePowerAction;
  triggerReboot: () => SettlePowerAction;
}

export const PowerActionContext = createContext<
  PowerActionContextType | undefined
>(undefined);
