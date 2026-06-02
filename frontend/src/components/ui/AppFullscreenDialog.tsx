import React, { useEffect, useEffectEvent, useRef } from "react";
import { createPortal } from "react-dom";

import "./app-fullscreen-dialog.css";

export interface AppFullscreenDialogProps {
  children?: React.ReactNode;
  className?: string;
  contentClassName?: string;
  contentStyle?: React.CSSProperties;
  disableEscapeKeyDown?: boolean;
  onClose?: () => void;
  open: boolean;
  style?: React.CSSProperties;
}

const OVERLAY_ROOT_SELECTOR = ".app-dialog-root, .app-fullscreen-dialog-root";

const AppFullscreenDialog: React.FC<AppFullscreenDialogProps> = ({
  open,
  onClose,
  disableEscapeKeyDown = false,
  children,
  className,
  style,
  contentClassName,
  contentStyle,
}) => {
  const rootRef = useRef<HTMLDivElement>(null);
  const lastFocusedElement = useRef<HTMLElement | null>(null);
  const previousBodyOverflow = useRef<string>("");

  useEffect(() => {
    if (!open) {
      return;
    }

    previousBodyOverflow.current = document.body.style.overflow;
    lastFocusedElement.current = document.activeElement as HTMLElement | null;
    document.body.style.overflow = "hidden";

    return () => {
      document.body.style.overflow = previousBodyOverflow.current;
      lastFocusedElement.current?.focus();
    };
  }, [open]);

  const handleDocumentKeyDown = useEffectEvent((event: KeyboardEvent) => {
    if (
      event.key !== "Escape" ||
      disableEscapeKeyDown ||
      event.defaultPrevented
    ) {
      return;
    }

    const root = rootRef.current;
    if (!root) {
      return;
    }

    const openOverlays = Array.from(
      document.querySelectorAll<HTMLDivElement>(OVERLAY_ROOT_SELECTOR),
    );
    if (openOverlays[openOverlays.length - 1] !== root) {
      return;
    }

    event.preventDefault();
    event.stopPropagation();
    onClose?.();
  });

  useEffect(() => {
    if (!open) {
      return;
    }

    document.addEventListener("keydown", handleDocumentKeyDown);
    return () => {
      document.removeEventListener("keydown", handleDocumentKeyDown);
    };
  }, [open]);

  useEffect(() => {
    if (!open || !rootRef.current) {
      return;
    }

    const focusable = rootRef.current.querySelector<HTMLElement>(
      "[autofocus], input, button, [tabindex]:not([tabindex='-1'])",
    );
    if (focusable) {
      focusable.focus();
    } else {
      rootRef.current.focus();
    }
  }, [open]);

  if (!open) {
    return null;
  }

  return createPortal(
    <div
      aria-modal="true"
      className={`app-fullscreen-dialog-root ${className || ""}`.trim()}
      ref={rootRef}
      role="dialog"
      style={style}
      tabIndex={-1}
    >
      <div
        className={`app-fullscreen-dialog__content ${contentClassName || ""}`.trim()}
        style={contentStyle}
      >
        {children}
      </div>
    </div>,
    document.body,
  );
};

export default AppFullscreenDialog;
