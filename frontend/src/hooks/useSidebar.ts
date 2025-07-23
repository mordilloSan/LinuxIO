import { useContext } from "react";
import { SidebarContext, SidebarContextType } from "@/contexts/SidebarContext";

const useSidebar = (): SidebarContextType => {
  const context = useContext(SidebarContext);
  if (!context) {
    throw new Error("useSidebar must be used within a SidebarProvider");
  }
  return context;
};

export default useSidebar;
