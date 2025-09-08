// src/contexts/WebSocketContext.tsx
import React, {
  createContext,
  useRef,
  useCallback,
  useLayoutEffect,
  useState,
} from "react";

type WebSocketMessage = any;
type MessageHandler = (msg: WebSocketMessage) => void;

type WSStatus = "idle" | "connecting" | "open" | "closed";

interface WebSocketContextValue {
  send: (msg: any) => void;
  subscribe: (handler: MessageHandler) => () => void;
  status: WSStatus;
}

export const WebSocketContext = createContext<WebSocketContextValue | null>(
  null,
);

export const WebSocketProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [status, setStatus] = useState<WSStatus>("idle");
  const wsRef = useRef<WebSocket | null>(null);
  const handlers = useRef<Set<MessageHandler>>(new Set());

  useLayoutEffect(() => {
    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    const wsUrl = `${proto}://${window.location.host}/ws`;

    setStatus("connecting");
    const ws = new window.WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => setStatus("open");
    ws.onclose = () => setStatus("closed");
    ws.onerror = () => setStatus("closed");

    ws.onmessage = (event) => {
      let msg: any;
      try {
        msg = JSON.parse(event.data);
      } catch {
        msg = { type: "raw", data: event.data };
      }
      handlers.current.forEach((fn) => fn(msg));
    };

    return () => {
      try {
        ws.close();
      } catch {
        /* ignore */
      }
      wsRef.current = null;
      setStatus("closed");
    };
  }, []);

  const send = useCallback((msg: any) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(typeof msg === "string" ? msg : JSON.stringify(msg));
    }
  }, []);

  const subscribe = useCallback((fn: MessageHandler) => {
    handlers.current.add(fn);
    return () => {
      handlers.current.delete(fn);
    };
  }, []);

  return (
    <WebSocketContext.Provider value={{ send, subscribe, status }}>
      {children}
    </WebSocketContext.Provider>
  );
};
