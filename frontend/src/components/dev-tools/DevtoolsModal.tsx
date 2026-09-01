import { Icon } from "@iconify/react";
import { useRef, useState, type PointerEvent, type ReactNode } from "react";

import { AppDialog } from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";

interface DevtoolsModalProps {
  children: ReactNode;
  onClose: () => void;
  onExited?: () => void;
  open: boolean;
}

const INITIAL_WIDTH = 1000;
const INITIAL_HEIGHT = 500;

export function DevtoolsModal({
  children,
  onClose,
  onExited,
  open,
}: DevtoolsModalProps) {
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
    <AppDialog
      aria-label="Development tools"
      maxWidth={false}
      onClose={onClose}
      open={open}
      paperStyle={{
        borderRadius: "var(--app-radius-lg)",
        height: "100%",
        overflow: "hidden",
        width: "100%",
      }}
      style={{
        display: "flex",
        height: `${INITIAL_HEIGHT}px`,
        left: `${position.x}px`,
        margin: 0,
        maxHeight: "calc(100vh - 40px)",
        maxWidth: "calc(100vw - 40px)",
        minHeight: "240px",
        minWidth: "320px",
        overflow: "hidden",
        position: "fixed",
        resize: "both",
        top: `${position.y}px`,
        width: `${INITIAL_WIDTH}px`,
      }}
      slotProps={{ transition: { onExited } }}
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
      {/* `app-scrollbar-nested` reaches the devtools' own scroll panes,
            which are library-owned DOM we cannot put a class on. */}
      <div
        className="app-scrollbar-nested"
        data-testid="devtools-modal-content"
        style={{ flex: 1, minHeight: 0, minWidth: 0, overflow: "auto" }}
      >
        {children}
      </div>
    </AppDialog>
  );
}
