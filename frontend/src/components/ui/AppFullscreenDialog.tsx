import { AnimatePresence, motion, type Variants } from "motion/react";
import {
  useEffect,
  useEffectEvent,
  useRef,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

import {
  EASING_DECELERATE,
  TRANSITION_DURATION_FAST_MS,
  TRANSITION_DURATION_STANDARD_MS,
} from "@/theme/constants";

import { OVERLAY_ROOT_SELECTOR } from "./AppDialog";
import { acquireBodyScrollLock } from "./bodyScrollLock";
import { useDialogFocusRestore } from "./useDialogFocusRestore";

import "./app-fullscreen-dialog.css";

const fullscreenDialogVariants = {
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

export interface AppFullscreenDialogProps {
  children?: ReactNode;
  className?: string;
  contentClassName?: string;
  contentStyle?: CSSProperties;
  disableEscapeKeyDown?: boolean;
  onClose?: () => void;
  open: boolean;
  slotProps?: {
    transition?: { onEntered?: () => void; onExited?: () => void };
  };
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
  slotProps,
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

  return createPortal(
    <AnimatePresence onExitComplete={slotProps?.transition?.onExited}>
      {open && (
        <motion.div
          animate="visible"
          aria-modal="true"
          className={`app-fullscreen-dialog-root ${className || ""}`.trim()}
          exit="exit"
          initial="hidden"
          key="fullscreen-dialog"
          onAnimationComplete={(definition) => {
            if (definition === "visible") {
              slotProps?.transition?.onEntered?.();
            }
          }}
          ref={rootRef}
          role="dialog"
          style={style}
          tabIndex={-1}
          variants={fullscreenDialogVariants}
        >
          <div
            className={`app-fullscreen-dialog__content ${contentClassName || ""}`.trim()}
            style={contentStyle}
          >
            {children}
          </div>
        </motion.div>
      )}
    </AnimatePresence>,
    document.body,
  );
};

export default AppFullscreenDialog;
