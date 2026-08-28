import { AnimatePresence, motion, type Variants } from "motion/react";
import {
  useEffect,
  useEffectEvent,
  useRef,
  type CSSProperties,
  type HTMLAttributes,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
  type Ref,
} from "react";
import { createPortal } from "react-dom";

import {
  EASING_DECELERATE,
  TRANSITION_DURATION_FAST_MS,
  TRANSITION_DURATION_STANDARD_MS,
} from "@/theme/constants";

import { acquireBodyScrollLock } from "./bodyScrollLock";
import { useDialogFocusRestore } from "./useDialogFocusRestore";

import "./app-dialog.css";

let _openDialogCount = 0;

const backdropVariants = {
  hidden: { opacity: 0 },
  visible: {
    opacity: 1,
    transition: {
      duration: TRANSITION_DURATION_STANDARD_MS / 1000,
      ease: EASING_DECELERATE,
    },
  },
  exit: {
    opacity: 0,
    transition: {
      duration: TRANSITION_DURATION_FAST_MS / 1000,
      ease: EASING_DECELERATE,
    },
  },
} satisfies Variants;

const dialogVariants = {
  hidden: { opacity: 0, y: 12 },
  visible: {
    opacity: 1,
    y: 0,
    transition: {
      duration: TRANSITION_DURATION_STANDARD_MS / 1000,
      ease: EASING_DECELERATE,
    },
  },
  exit: {
    opacity: 0,
    y: -12,
    transition: {
      duration: TRANSITION_DURATION_FAST_MS / 1000,
      ease: EASING_DECELERATE,
    },
  },
} satisfies Variants;

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
  "aria-label"?: string;
  /** Reports an in-flight action owned by the mounted dialog. */
  "aria-busy"?: boolean;
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
  "aria-label": ariaLabel,
  "aria-busy": ariaBusy,
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

  useDialogFocusRestore(open);

  // scroll lock + background blur class
  useEffect(() => {
    if (!open) {
      return;
    }
    const releaseBodyScrollLock = acquireBodyScrollLock();
    _openDialogCount += 1;
    if (_openDialogCount === 1) document.body.classList.add("dialog-open");

    return () => {
      releaseBodyScrollLock();
      _openDialogCount -= 1;
      if (_openDialogCount === 0) document.body.classList.remove("dialog-open");
    };
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

  const sizeClass = maxWidth ? `app-dialog--${maxWidth}` : "";
  const widthClass = fullWidth ? "app-dialog--fullwidth" : "";

  const mergedPaperStyle = {
    ...paperStyle,
    ...slotProps?.paper?.style,
  };
  const mergedPaperClass = [
    "app-dialog__paper",
    "custom-scrollbar",
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
    <AnimatePresence onExitComplete={slotProps?.transition?.onExited}>
      {open && (
        <motion.div
          animate="visible"
          className="app-dialog-root"
          exit="exit"
          initial="hidden"
          key="dialog"
          // React synthetic events bubble through the React tree, so a dialog
          // rendered inside e.g. a clickable card would still trigger that card's
          // onClick / onMouseDown. Stop those at the portal root.
          onClick={(event) => event.stopPropagation()}
          onMouseDown={(event) => event.stopPropagation()}
          ref={rootRef}
          role="presentation"
        >
          <motion.div
            aria-hidden
            className="app-dialog__backdrop"
            onClick={(event) => onClose?.(event, "backdropClick")}
            style={mergedBackdropStyle}
            variants={backdropVariants}
          />
          <motion.div
            aria-label={ariaLabel}
            aria-busy={ariaBusy || undefined}
            aria-modal="true"
            className={`app-dialog ${sizeClass} ${widthClass} ${className || ""}`.trim()}
            onAnimationComplete={(definition) => {
              if (definition === "visible") {
                slotProps?.transition?.onEntered?.();
              }
            }}
            ref={dialogRef}
            role="dialog"
            style={style}
            tabIndex={-1}
            variants={dialogVariants}
          >
            <div className={mergedPaperClass} style={mergedPaperStyle}>
              {children}
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>,
    document.body,
  );
};

/* ── DialogTitle ────────────────────────────── */

interface AppDialogTitleProps extends HTMLAttributes<HTMLDivElement> {
  children?: ReactNode;
  ref?: Ref<HTMLDivElement>;
}

export const AppDialogTitle = ({
  ref,
  className,
  ...props
}: AppDialogTitleProps) => (
  <div
    className={`app-dialog-title ${className || ""}`.trim()}
    ref={ref}
    {...props}
  />
);
AppDialogTitle.displayName = "AppDialogTitle";

/* ── DialogContent ──────────────────────────── */

interface AppDialogContentProps extends HTMLAttributes<HTMLDivElement> {
  children?: ReactNode;
  ref?: Ref<HTMLDivElement>;
}

export const AppDialogContent = ({
  ref,
  className,
  ...props
}: AppDialogContentProps) => (
  <div
    className={`app-dialog-content custom-scrollbar ${className || ""}`.trim()}
    ref={ref}
    {...props}
  />
);
AppDialogContent.displayName = "AppDialogContent";

/* ── DialogContentText ──────────────────────── */

interface AppDialogContentTextProps extends HTMLAttributes<HTMLParagraphElement> {
  children?: ReactNode;
  ref?: Ref<HTMLParagraphElement>;
}

export const AppDialogContentText = ({
  ref,
  className,
  ...props
}: AppDialogContentTextProps) => (
  <p
    className={`app-dialog-content-text ${className || ""}`.trim()}
    ref={ref}
    {...props}
  />
);
AppDialogContentText.displayName = "AppDialogContentText";

/* ── DialogActions ──────────────────────────── */

interface AppDialogActionsProps extends HTMLAttributes<HTMLDivElement> {
  children?: ReactNode;
  ref?: Ref<HTMLDivElement>;
}

export const AppDialogActions = ({
  ref,
  className,
  ...props
}: AppDialogActionsProps) => (
  <div
    className={`app-dialog-actions ${className || ""}`.trim()}
    ref={ref}
    {...props}
  />
);
AppDialogActions.displayName = "AppDialogActions";
