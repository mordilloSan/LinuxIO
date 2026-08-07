import {
  forwardRef,
  HTMLAttributes,
  useEffect,
  useEffectEvent,
  useRef,
  type CSSProperties,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

import { acquireBodyScrollLock } from "./bodyScrollLock";

import "./app-dialog.css";

let _openDialogCount = 0;

// Overlay portals that participate in the Escape stack: only the last one in
// DOM order may close on Escape. Shared with AppFullscreenDialog and with
// document-level shortcut hooks that must stay quiet while an overlay is open.
export const OVERLAY_ROOT_SELECTOR =
  ".app-dialog-root, .app-fullscreen-dialog-root";

/* ── Dialog ─────────────────────────────────── */

export type AppDialogCloseEvent =
  | globalThis.KeyboardEvent
  | KeyboardEvent<HTMLDivElement>
  | MouseEvent<HTMLDivElement>;

export interface AppDialogProps {
  /** Styles applied to the backdrop overlay */
  backdropStyle?: CSSProperties;
  children?: ReactNode;
  className?: string;
  /** When true, pressing Escape will not close the dialog */
  disableEscapeKeyDown?: boolean;
  fullWidth?: boolean;
  maxWidth?: "xs" | "sm" | "md" | "lg" | "xl" | false;
  onClose?: (
    event: AppDialogCloseEvent,
    reason: "backdropClick" | "escapeKeyDown",
  ) => void;
  open: boolean;
  /** Class name applied to the paper element */
  paperClassName?: string;
  /** Styles applied to the paper (content wrapper) element */
  paperStyle?: CSSProperties;
  /** Slot props for advanced customization */
  slotProps?: {
    paper?: { style?: CSSProperties; className?: string };
    backdrop?: { style?: CSSProperties };
    transition?: { onEntered?: () => void; onExited?: () => void };
  };
  style?: CSSProperties;
}

export const AppDialog = ({
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
}: AppDialogProps) => {
  const rootRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const prevOpen = useRef(open);
  const lastFocusedElement = useRef<HTMLElement | null>(null);

  // scroll lock + background blur class
  useEffect(() => {
    if (open) {
      lastFocusedElement.current = document.activeElement as HTMLElement | null;
      const releaseBodyScrollLock = acquireBodyScrollLock();
      _openDialogCount++;
      if (_openDialogCount === 1) document.body.classList.add("dialog-open");

      return () => {
        releaseBodyScrollLock();
        _openDialogCount--;
        if (_openDialogCount === 0)
          document.body.classList.remove("dialog-open");
      };
    } else if (lastFocusedElement.current) {
      lastFocusedElement.current.focus();
    }
  }, [open]);

  // fire transition callbacks
  const fireTransition = useEffectEvent((didOpen: boolean) => {
    if (didOpen) {
      slotProps?.transition?.onEntered?.();
    } else {
      slotProps?.transition?.onExited?.();
    }
  });

  useEffect(() => {
    if (open && !prevOpen.current) {
      fireTransition(true);
    } else if (!open && prevOpen.current) {
      fireTransition(false);
    }
    prevOpen.current = open;
  }, [open]);

  // ESC key
  const handleDocumentKeyDown = useEffectEvent(
    (event: globalThis.KeyboardEvent) => {
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
      onClose?.(event, "escapeKeyDown");
    },
  );

  useEffect(() => {
    if (!open) {
      return;
    }

    document.addEventListener("keydown", handleDocumentKeyDown);
    return () => {
      document.removeEventListener("keydown", handleDocumentKeyDown);
    };
  }, [open]);

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
    <div
      className="app-dialog-root"
      // React synthetic events bubble through the React tree, so a dialog
      // rendered inside e.g. a clickable card would still trigger that card's
      // onClick / onMouseDown. Stop those at the portal root.
      onClick={(e) => e.stopPropagation()}
      onMouseDown={(e) => e.stopPropagation()}
      ref={rootRef}
      role="presentation"
    >
      <div
        aria-hidden
        className="app-dialog__backdrop"
        onClick={(e) => onClose?.(e, "backdropClick")}
        style={mergedBackdropStyle}
      />
      <div
        aria-modal="true"
        className={`app-dialog ${sizeClass} ${widthClass} ${className || ""}`.trim()}
        ref={dialogRef}
        role="dialog"
        style={style}
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

interface AppDialogTitleProps extends HTMLAttributes<HTMLDivElement> {
  children?: ReactNode;
}

export const AppDialogTitle = forwardRef<HTMLDivElement, AppDialogTitleProps>(
  ({ className, ...props }, ref) => (
    <div
      className={`app-dialog-title ${className || ""}`.trim()}
      ref={ref}
      {...props}
    />
  ),
);
AppDialogTitle.displayName = "AppDialogTitle";

/* ── DialogContent ──────────────────────────── */

interface AppDialogContentProps extends HTMLAttributes<HTMLDivElement> {
  children?: ReactNode;
}

export const AppDialogContent = forwardRef<
  HTMLDivElement,
  AppDialogContentProps
>(({ className, ...props }, ref) => (
  <div
    className={`app-dialog-content ${className || ""}`.trim()}
    ref={ref}
    {...props}
  />
));
AppDialogContent.displayName = "AppDialogContent";

/* ── DialogContentText ──────────────────────── */

interface AppDialogContentTextProps extends HTMLAttributes<HTMLParagraphElement> {
  children?: ReactNode;
}

export const AppDialogContentText = forwardRef<
  HTMLParagraphElement,
  AppDialogContentTextProps
>(({ className, ...props }, ref) => (
  <p
    className={`app-dialog-content-text ${className || ""}`.trim()}
    ref={ref}
    {...props}
  />
));
AppDialogContentText.displayName = "AppDialogContentText";

/* ── DialogActions ──────────────────────────── */

interface AppDialogActionsProps extends HTMLAttributes<HTMLDivElement> {
  children?: ReactNode;
}

export const AppDialogActions = forwardRef<
  HTMLDivElement,
  AppDialogActionsProps
>(({ className, ...props }, ref) => (
  <div
    className={`app-dialog-actions ${className || ""}`.trim()}
    ref={ref}
    {...props}
  />
));
AppDialogActions.displayName = "AppDialogActions";
