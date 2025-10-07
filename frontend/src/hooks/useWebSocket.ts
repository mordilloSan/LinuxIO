import { useContext } from "react";
import { WebSocketContext } from "@/contexts/WebSocketContext";

const useWebSocket = () => {
  const context = useContext(WebSocketContext);
  if (!context)
    throw new Error("useAppWebSocket must be used within WebSocketProvider");
  return context;
};

export default useWebSocket;
