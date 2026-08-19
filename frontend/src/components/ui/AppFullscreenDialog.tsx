import {
  useEffect,
  useEffectEvent,
  useRef,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

import { OVERLAY_ROOT_SELECTOR } from "./AppDialog";
import { acquireBodyScrollLock } from "./bodyScrollLock";
import { useDialogFocusRestore } from "./useDialogFocusRestore";

import "./app-fullscreen-dialog.css";

export interface AppFullscreenDialogProps {
  children?: ReactNode;
  className?: string;
  contentClassName?: string;
  contentStyle?: CSSProperties;
  disableEscapeKeyDown?: boolean;
  onClose?: () => void;
  open: boolean;
  style?: CSSProperties;
}

const AppFullscreenDialog = ({
  open,
  onClose,
  disableEscapeKeyDown = false,
  children,
  className,
  style,
  contentClassName,
  contentStyle,
}: AppFullscreenDialogProps) => {
  const rootRef = useRef<HTMLDivElement>(null);

  useDialogFocusRestore(open);

  useEffect(() => {
    if (!open) {
      return;
    }

    const releaseBodyScrollLock = acquireBodyScrollLock();
    return releaseBodyScrollLock;
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
