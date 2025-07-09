import React, {
  createContext,
  useContext,
  useRef,
  useCallback,
  useEffect,
  useState,
} from "react";

type WebSocketMessage = any;
type MessageHandler = (msg: WebSocketMessage) => void;

interface WebSocketContextValue {
  send: (msg: any) => void;
  subscribe: (handler: MessageHandler) => () => void;
  ready: boolean;
}

const WebSocketContext = createContext<WebSocketContextValue | null>(null);

export const WebSocketProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [ready, setReady] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const handlers = useRef<Set<MessageHandler>>(new Set());

  useEffect(() => {
    const wsUrl = import.meta.env.DEV
      ? "ws://localhost:8080/ws"
      : window.location.protocol === "https:"
        ? `wss://${window.location.host}/ws`
        : `ws://${window.location.host}/ws`;

    const ws = new window.WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => setReady(true);
    ws.onclose = () => setReady(false);
    ws.onerror = () => setReady(false);

    ws.onmessage = (event) => {
      let msg;
      try {
        msg = JSON.parse(event.data);
      } catch {
        msg = { type: "raw", data: event.data };
      }
      handlers.current.forEach((fn) => fn(msg));
    };

    return () => {
      ws.close();
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
    <WebSocketContext.Provider value={{ send, subscribe, ready }}>
      {children}
    </WebSocketContext.Provider>
  );
};

export function useAppWebSocket() {
  const ctx = useContext(WebSocketContext);
  if (!ctx)
    throw new Error("useAppWebSocket must be used inside <WebSocketProvider>");
  return ctx;
}
