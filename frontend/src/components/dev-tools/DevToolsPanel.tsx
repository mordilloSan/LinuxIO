import { Button } from "@mui/material";
import { ReactQueryDevtoolsPanel } from "@tanstack/react-query-devtools";
import { useState, useRef, useEffect } from "react";

interface DevToolsPanelProps {
  isOpen: boolean;
  onClose: () => void;
}

/**
 * Dev-only tool panel for testing and debugging.
 * Only rendered when import.meta.env.DEV is true.
 */
export const DevToolsPanel = ({ isOpen, onClose }: DevToolsPanelProps) => {
  // Check if update notification is currently shown
  const shown = !!sessionStorage.getItem("dev_update_forced");
  const [isDevtoolsOpen, setIsDevtoolsOpen] = useState(false);

  // Draggable state for devtools - initialize to center of screen
  const [position, setPosition] = useState(() => ({
    x: Math.max(20, (window.innerWidth - 600) / 2),
    y: Math.max(20, (window.innerHeight - 500) / 2),
  }));
  const [isDragging, setIsDragging] = useState(false);
  const dragRef = useRef<{ startX: number; startY: number; initialX: number; initialY: number } | null>(null);

  const forceUpdateNotification = () => {
    const fakeUpdateInfo = {
      available: true,
      current_version: "dev-v0.6.12",
      latest_version: "dev-v0.6.12",
      release_url: "https://github.com/mordilloSan/LinuxIO/releases",
    };

    sessionStorage.setItem("update_info", JSON.stringify(fakeUpdateInfo));
    sessionStorage.setItem("dev_update_forced", "true");
    window.location.reload();
  };

  const clearUpdateNotification = () => {
    sessionStorage.removeItem("update_info");
    sessionStorage.removeItem("dev_update_forced");
    window.location.reload();
  };

  const handleMouseDown = (e: React.MouseEvent) => {
    if ((e.target as HTMLElement).closest('.drag-handle')) {
      setIsDragging(true);
      dragRef.current = {
        startX: e.clientX,
        startY: e.clientY,
        initialX: position.x,
        initialY: position.y,
      };
      e.preventDefault();
    }
  };

  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (isDragging && dragRef.current) {
        const deltaX = e.clientX - dragRef.current.startX;
        const deltaY = e.clientY - dragRef.current.startY;

        const newX = dragRef.current.initialX + deltaX;
        const newY = dragRef.current.initialY + deltaY;

        // Constrain to viewport - keep at least 50px of the panel visible
        const minVisible = 50;
        const maxX = window.innerWidth - minVisible;
        const maxY = window.innerHeight - minVisible;

        const constrainedX = Math.max(-550, Math.min(maxX, newX));
        const constrainedY = Math.max(0, Math.min(maxY, newY));

        setPosition({ x: constrainedX, y: constrainedY });
      }
    };

    const handleMouseUp = () => {
      setIsDragging(false);
      dragRef.current = null;
    };

    if (isDragging) {
      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
      return () => {
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
      };
    }
  }, [isDragging, position.x, position.y]);

  if (!import.meta.env.DEV || !isOpen) {
    return null;
  }

  return (
    <>
      {/* Dev Tools Panel */}
      <div
        style={{
          position: "fixed",
          bottom: 60,
          right: 20,
          zIndex: 9999,
          color: "white",
          padding: "12px 16px",
          borderRadius: 8,
          boxShadow: "0 4px 6px rgba(0,0,0,0.3)",
          fontSize: 14,
          display: "flex",
          flexDirection: "column",
          gap: 8,
          minWidth: 200,
        }}
      >
        <div style={{ fontWeight: "bold", marginBottom: 4, display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <span>🛠️ Dev Tools</span>
          <button
            onClick={onClose}
            style={{
              background: "transparent",
              border: "none",
              color: "white",
              cursor: "pointer",
              fontSize: 18,
              padding: 0,
              marginLeft: 8,
            }}
          >
            ×
          </button>
        </div>
        <Button
          variant="contained"
          color="primary"
          size="small"
          onClick={() => setIsDevtoolsOpen(!isDevtoolsOpen)}
          fullWidth
        >
          {isDevtoolsOpen ? "Close" : "Open"} React Query Devtools
        </Button>
        {!shown ? (
          <Button
            variant="contained"
            color="warning"
            size="small"
            onClick={forceUpdateNotification}
            fullWidth
          >
            Show Update Notification
          </Button>
        ) : (
          <Button
            variant="contained"
            color="secondary"
            size="small"
            onClick={clearUpdateNotification}
            fullWidth
          >
            Hide Update Notification
          </Button>
        )}
      </div>

      {/* Draggable Devtools Panel */}
      {isDevtoolsOpen && (
        <>
          {/* Backdrop */}
          <div
            style={{
              position: "fixed",
              top: 0,
              left: 0,
              width: "100%",
              height: "100%",
              backgroundColor: "rgba(0, 0, 0, 0.5)",
              zIndex: 9997,
            }}
            onClick={() => setIsDevtoolsOpen(false)}
          />
          {/* Draggable Devtools Panel */}
          <div
            onMouseDown={handleMouseDown}
            style={{
              position: "fixed",
              top: `${position.y}px`,
              left: `${position.x}px`,
              width: "600px",
              height: "500px",
              zIndex: 9998,
              borderRadius: 12,
              overflow: "hidden",
              boxShadow: "0 25px 50px -12px rgba(0, 0, 0, 0.5)",
              display: "flex",
              flexDirection: "column",
              cursor: isDragging ? "grabbing" : "default",
            }}
          >
            {/* Drag Handle */}
            <div
              className="drag-handle"
              style={{
                background: "linear-gradient(135deg, #667eea 0%, #764ba2 100%)",
                padding: "8px 12px",
                color: "white",
                fontWeight: "bold",
                cursor: "grab",
                userSelect: "none",
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
              }}
            >
              <span>React Query Devtools (Drag to move)</span>
              <button
                onClick={() => setIsDevtoolsOpen(false)}
                style={{
                  background: "rgba(255, 255, 255, 0.2)",
                  border: "none",
                  borderRadius: "4px",
                  color: "white",
                  cursor: "pointer",
                  fontSize: 16,
                  padding: "2px 8px",
                }}
              >
                ×
              </button>
            </div>
            <div style={{ flex: 1, overflow: "hidden" }}>
              <ReactQueryDevtoolsPanel onClose={() => setIsDevtoolsOpen(false)} />
            </div>
          </div>
        </>
      )}
    </>
  );
};
