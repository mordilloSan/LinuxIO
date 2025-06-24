// ContainerList.tsx
import React, { useEffect, useState } from "react";

import { useWebSocket } from "@/contexts/WebSocketContext";

const ContainerList: React.FC = () => {
  const { send, lastMessage } = useWebSocket();
  const [containers, setContainers] = useState<any[]>([]);

  // Request containers on mount
  useEffect(() => {
    send({
      type: "ListContainers", // match backend function
      payload: {}, // if needed
    });
  }, [send]);

  // Listen for a response (simple demo)
  useEffect(() => {
    if (lastMessage?.type === "ListContainers_response") {
      setContainers(lastMessage.data || []);
    }
  }, [lastMessage]);

  return (
    <div>
      <h2>Docker Containers (WebSocket)</h2>
      <pre style={{ fontSize: 12, maxHeight: 400, overflow: "auto" }}>
        {JSON.stringify(containers, null, 2)}
      </pre>
    </div>
  );
};

export default ContainerList;
