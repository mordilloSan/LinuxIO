import { useContext } from "react";

import { HeaderActionSlotContext } from "@/contexts/HeaderActionSlotContext";

export const useHeaderActionSlot = () => useContext(HeaderActionSlotContext);
