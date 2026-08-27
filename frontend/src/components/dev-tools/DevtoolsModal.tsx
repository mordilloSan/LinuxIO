import { Icon } from "@iconify/react";
import { useRef, useState, type PointerEvent, type ReactNode } from "react";

import AppIconButton from "@/components/ui/AppIconButton";

interface DevtoolsModalProps {
  children: ReactNode;
  onClose: () => void;
}

const INITIAL_WIDTH = 1000;
const INITIAL_HEIGHT = 500;

export function DevtoolsModal({ children, onClose }: DevtoolsModalProps) {
  const [position, setPosition] = useState(() => ({
    x: Math.max(20, (window.innerWidth - INITIAL_WIDTH) / 2),
    y: Math.max(20, (window.innerHeight - INITIAL_HEIGHT) / 2),
  }));
  const dragRef = useRef<{
    initialX: number;
    initialY: number;
    startX: number;
    startY: number;
  } | null>(null);

  const handleDragStart = (event: PointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = {
      startX: event.clientX,
      startY: event.clientY,
      initialX: position.x,
      initialY: position.y,
    };
  };

  const handleDragMove = (event: PointerEvent<HTMLDivElement>) => {
    if (!dragRef.current) return;

    const deltaX = event.clientX - dragRef.current.startX;
    const deltaY = event.clientY - dragRef.current.startY;
    const modal = event.currentTarget.parentElement;
    const width = modal?.offsetWidth || INITIAL_WIDTH;
    setPosition({
      x: Math.max(
        -width + 50,
        Math.min(window.innerWidth - 50, dragRef.current.initialX + deltaX),
      ),
      y: Math.max(
        0,
        Math.min(window.innerHeight - 50, dragRef.current.initialY + deltaY),
      ),
    });
  };

  const handleDragEnd = () => {
    dragRef.current = null;
  };

  return (
    <>
      <div
        aria-hidden="true"
        onClick={onClose}
        style={{
          position: "fixed",
          top: 0,
          left: 0,
          width: "100%",
          height: "100%",
          backgroundColor: "color-mix(in srgb, black, transparent 50%)",
          zIndex: 9997,
        }}
      />
      <div
        aria-label="Development tools"
        role="dialog"
        style={{
          position: "fixed",
          top: `${position.y}px`,
          left: `${position.x}px`,
          width: `${INITIAL_WIDTH}px`,
          height: `${INITIAL_HEIGHT}px`,
          minWidth: "320px",
          minHeight: "240px",
          maxWidth: "calc(100vw - 40px)",
          maxHeight: "calc(100vh - 40px)",
          zIndex: 9998,
          borderRadius: "var(--app-radius-lg)",
          overflow: "hidden",
          boxShadow:
            "0 25px 50px -12px color-mix(in srgb, black, transparent 50%)",
          backgroundColor: "var(--app-palette-background-paper)",
          display: "flex",
          flexDirection: "column",
          resize: "both",
        }}
      >
        <div
          aria-label="Drag development tools"
          onPointerCancel={handleDragEnd}
          onPointerDown={handleDragStart}
          onPointerMove={handleDragMove}
          onPointerUp={handleDragEnd}
          style={{
            cursor: "grab",
            flexShrink: 0,
            height: 28,
            position: "relative",
            touchAction: "none",
            userSelect: "none",
          }}
        >
          <AppIconButton
            aria-label="Close development tools"
            onClick={onClose}
            onPointerDown={(event) => event.stopPropagation()}
            style={{
              position: "absolute",
              top: 4,
              right: 6,
              background: "transparent",
              border: "none",
              color: "var(--app-palette-text-secondary)",
              cursor: "pointer",
              lineHeight: 1,
              padding: 2,
            }}
            type="button"
          >
            <Icon height={18} icon="mdi:close" width={18} />
          </AppIconButton>
        </div>
        {/* `custom-scrollbar-nested` reaches the devtools' own scroll panes,
            which are library-owned DOM we cannot put a class on. */}
        <div
          className="custom-scrollbar custom-scrollbar-nested"
          data-testid="devtools-modal-content"
          style={{ flex: 1, minHeight: 0, minWidth: 0, overflow: "auto" }}
        >
          {children}
        </div>
      </div>
    </>
  );
}
