import React, { useCallback, useEffect, useRef } from "react";
import { createPortal } from "react-dom";

import "./app-dialog.css";

/* ── Dialog ─────────────────────────────────── */

export type AppDialogCloseEvent =
  | KeyboardEvent
  | React.KeyboardEvent<HTMLDivElement>
  | React.MouseEvent<HTMLDivElement>;

export interface AppDialogProps {
  open: boolean;
  onClose?: (
    event: AppDialogCloseEvent,
    reason: "backdropClick" | "escapeKeyDown",
  ) => void;
  maxWidth?: "xs" | "sm" | "md" | "lg" | "xl" | false;
  fullWidth?: boolean;
  /** When true, pressing Escape will not close the dialog */
  disableEscapeKeyDown?: boolean;
  children?: React.ReactNode;
  className?: string;
  style?: React.CSSProperties;
  /** Styles applied to the paper (content wrapper) element */
  paperStyle?: React.CSSProperties;
  /** Class name applied to the paper element */
  paperClassName?: string;
  /** Styles applied to the backdrop overlay */
  backdropStyle?: React.CSSProperties;
  /** Slot props for advanced customization */
  slotProps?: {
    paper?: { style?: React.CSSProperties; className?: string };
    backdrop?: { style?: React.CSSProperties };
    transition?: { onEntered?: () => void; onExited?: () => void };
  };
}

export const AppDialog: React.FC<AppDialogProps> = ({
  open,
  onClose,
  maxWidth = "sm",
  fullWidth = false,
  disableEscapeKeyDown = false,
  children,
  className,
  style,
  paperStyle,
  paperClassName,
  backdropStyle,
  slotProps,
}) => {
  const rootRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const prevOpen = useRef(open);
  const lastFocusedElement = useRef<HTMLElement | null>(null);

  // scroll lock
  useEffect(() => {
    if (open) {
      lastFocusedElement.current = document.activeElement as HTMLElement | null;
      document.body.style.overflow = "hidden";
    } else if (lastFocusedElement.current) {
      lastFocusedElement.current.focus();
    }
    return () => {
      document.body.style.overflow = "";
    };
  }, [open]);

  // fire transition callbacks
  useEffect(() => {
    if (open && !prevOpen.current) {
      slotProps?.transition?.onEntered?.();
    }
    if (!open && prevOpen.current) {
      slotProps?.transition?.onExited?.();
    }
    prevOpen.current = open;
  }, [open, slotProps]);

  // ESC key
  const handleDocumentKeyDown = useCallback(
    (event: KeyboardEvent) => {
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

      const openDialogs = Array.from(
        document.querySelectorAll<HTMLDivElement>(".app-dialog-root"),
      );
      if (openDialogs[openDialogs.length - 1] !== root) {
        return;
      }

      event.preventDefault();
      event.stopPropagation();
      onClose?.(event, "escapeKeyDown");
    },
    [disableEscapeKeyDown, onClose],
  );

  useEffect(() => {
    if (!open) {
      return;
    }

    document.addEventListener("keydown", handleDocumentKeyDown);
    return () => {
      document.removeEventListener("keydown", handleDocumentKeyDown);
    };
  }, [handleDocumentKeyDown, open]);

  // auto-focus
  useEffect(() => {
    if (open && dialogRef.current) {
      const focusable = dialogRef.current.querySelector<HTMLElement>(
        "[autofocus], input, button, [tabindex]",
      );
      if (focusable) {
        focusable.focus();
      } else {
        dialogRef.current.focus();
      }
    }
  }, [open]);

  if (!open) return null;

  const sizeClass = maxWidth ? `app-dialog--${maxWidth}` : "";
  const widthClass = fullWidth ? "app-dialog--fullwidth" : "";

  const mergedPaperStyle = {
    ...paperStyle,
    ...slotProps?.paper?.style,
  };
  const mergedPaperClass = [
    "app-dialog__paper",
    paperClassName,
    slotProps?.paper?.className,
  ]
    .filter(Boolean)
    .join(" ");

  const mergedBackdropStyle = {
    ...backdropStyle,
    ...slotProps?.backdrop?.style,
  };

  return createPortal(
    <div ref={rootRef} className="app-dialog-root" role="presentation">
      <div
        className="app-dialog__backdrop"
        style={mergedBackdropStyle}
        onClick={(e) => onClose?.(e, "backdropClick")}
        aria-hidden
      />
      <div
        ref={dialogRef}
        className={`app-dialog ${sizeClass} ${widthClass} ${className || ""}`.trim()}
        style={style}
        role="dialog"
        aria-modal="true"
        tabIndex={-1}
      >
        <div className={mergedPaperClass} style={mergedPaperStyle}>
          {children}
        </div>
      </div>
    </div>,
    document.body,
  );
};

/* ── DialogTitle ────────────────────────────── */

interface AppDialogTitleProps extends React.HTMLAttributes<HTMLDivElement> {
  children?: React.ReactNode;
}

export const AppDialogTitle = React.forwardRef<
  HTMLDivElement,
  AppDialogTitleProps
>(({ className, ...props }, ref) => (
  <div
    ref={ref}
    className={`app-dialog-title ${className || ""}`.trim()}
    {...props}
  />
));
AppDialogTitle.displayName = "AppDialogTitle";

/* ── DialogContent ──────────────────────────── */

interface AppDialogContentProps extends React.HTMLAttributes<HTMLDivElement> {
  children?: React.ReactNode;
}

export const AppDialogContent = React.forwardRef<
  HTMLDivElement,
  AppDialogContentProps
>(({ className, ...props }, ref) => (
  <div
    ref={ref}
    className={`app-dialog-content ${className || ""}`.trim()}
    {...props}
  />
));
AppDialogContent.displayName = "AppDialogContent";

/* ── DialogContentText ──────────────────────── */

interface AppDialogContentTextProps extends React.HTMLAttributes<HTMLParagraphElement> {
  children?: React.ReactNode;
}

export const AppDialogContentText = React.forwardRef<
  HTMLParagraphElement,
  AppDialogContentTextProps
>(({ className, ...props }, ref) => (
  <p
    ref={ref}
    className={`app-dialog-content-text ${className || ""}`.trim()}
    {...props}
  />
));
AppDialogContentText.displayName = "AppDialogContentText";

/* ── DialogActions ──────────────────────────── */

interface AppDialogActionsProps extends React.HTMLAttributes<HTMLDivElement> {
  children?: React.ReactNode;
}

export const AppDialogActions = React.forwardRef<
  HTMLDivElement,
  AppDialogActionsProps
>(({ className, ...props }, ref) => (
  <div
    ref={ref}
    className={`app-dialog-actions ${className || ""}`.trim()}
    {...props}
  />
));
AppDialogActions.displayName = "AppDialogActions";
