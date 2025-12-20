import { useContext } from "react";

import {
  PowerActionContext,
  PowerActionContextType,
} from "@/contexts/PowerActionContext";

const usePowerAction = (): PowerActionContextType => {
  const context = useContext(PowerActionContext);
  if (!context) {
    throw new Error("usePowerAction must be used within a PowerActionProvider");
  }
  return context;
};

export default usePowerAction;
