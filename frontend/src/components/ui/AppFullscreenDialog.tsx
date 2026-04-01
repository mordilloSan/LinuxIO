import React, { useEffect, useEffectEvent, useRef } from "react";
import { createPortal } from "react-dom";

import "./app-fullscreen-dialog.css";

export interface AppFullscreenDialogProps {
  open: boolean;
  onClose?: () => void;
  disableEscapeKeyDown?: boolean;
  children?: React.ReactNode;
  className?: string;
  style?: React.CSSProperties;
  contentClassName?: string;
  contentStyle?: React.CSSProperties;
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
      ref={rootRef}
      className={`app-fullscreen-dialog-root ${className || ""}`.trim()}
      style={style}
      role="dialog"
      aria-modal="true"
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
