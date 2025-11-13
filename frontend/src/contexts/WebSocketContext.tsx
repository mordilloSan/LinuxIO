// src/contexts/WebSocketContext.tsx
import React, {
  createContext,
  useRef,
  useCallback,
  useState,
  useEffect,
} from "react";
import { useLocation } from "react-router-dom";

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

// Extract route name from pathname
function getRouteFromPathname(pathname: string): string {
  const parts = pathname.split("/").filter(Boolean);
  if (!parts.length) return "terminal"; // default route
  return parts[0];
}

export const WebSocketProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [status, setStatus] = useState<WSStatus>(() =>
    typeof window === "undefined" ? "idle" : "connecting",
  );
  const wsRef = useRef<WebSocket | null>(null);
  const handlers = useRef<Set<MessageHandler>>(new Set());
  const location = useLocation();
  const initialRoute = getRouteFromPathname(location.pathname);
  const currentRouteRef = useRef<string>(initialRoute);

  // Initial WebSocket connection
  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }
    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    const route = currentRouteRef.current;
    const wsUrl = `${proto}://${window.location.host}/ws?route=${encodeURIComponent(route)}`;

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
    };
  }, []);

  // Detect route changes and send route_change message
  useEffect(() => {
    const newRoute = getRouteFromPathname(location.pathname);

    // Only send if route actually changed and WebSocket is open
    if (
      newRoute !== currentRouteRef.current &&
      wsRef.current?.readyState === WebSocket.OPEN
    ) {
      currentRouteRef.current = newRoute;
      wsRef.current.send(
        JSON.stringify({
          type: "route_change",
          data: newRoute,
        }),
      );
    }
  }, [location.pathname]);

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
