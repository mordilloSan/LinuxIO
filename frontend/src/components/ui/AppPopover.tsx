import React, {
  useCallback,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";

import "./app-popover.css";

type VerticalOrigin = "top" | "center" | "bottom";
type HorizontalOrigin = "left" | "center" | "right";

export interface AppPopoverOrigin {
  vertical: VerticalOrigin;
  horizontal: HorizontalOrigin;
}

export interface AppPopoverProps {
  open: boolean;
  onClose?: () => void;
  anchorEl?: HTMLElement | null;
  anchorPosition?: { top: number; left: number } | null;
  anchorOrigin?: AppPopoverOrigin;
  transformOrigin?: AppPopoverOrigin;
  matchAnchorWidth?: boolean;
  children: React.ReactNode;
  className?: string;
  paperClassName?: string;
  style?: React.CSSProperties;
  paperStyle?: React.CSSProperties;
  paperRef?: React.Ref<HTMLDivElement>;
  zIndex?: number;
}

const DEFAULT_ORIGIN: AppPopoverOrigin = {
  vertical: "top",
  horizontal: "left",
};

const VIEWPORT_MARGIN = 8;

const getHorizontalOffset = (origin: HorizontalOrigin, width: number) => {
  switch (origin) {
    case "center":
      return width / 2;
    case "right":
      return width;
    default:
      return 0;
  }
};

const getVerticalOffset = (origin: VerticalOrigin, height: number) => {
  switch (origin) {
    case "center":
      return height / 2;
    case "bottom":
      return height;
    default:
      return 0;
  }
};

const mergeRefs = <T,>(
  refs: Array<React.Ref<T> | undefined>,
  value: T | null,
) => {
  refs.forEach((ref) => {
    if (!ref) return;
    if (typeof ref === "function") {
      ref(value);
      return;
    }
    ref.current = value;
  });
};

const AppPopover: React.FC<AppPopoverProps> = ({
  open,
  onClose,
  anchorEl,
  anchorPosition,
  anchorOrigin = DEFAULT_ORIGIN,
  transformOrigin = DEFAULT_ORIGIN,
  matchAnchorWidth = false,
  children,
  className,
  paperClassName,
  style,
  paperStyle,
  paperRef,
  zIndex = 1400,
}) => {
  const internalPaperRef = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState({ top: -9999, left: -9999 });

  const setPaperRef = useCallback(
    (node: HTMLDivElement | null) => {
      mergeRefs([internalPaperRef, paperRef], node);
    },
    [paperRef],
  );

  const updatePosition = useCallback(() => {
    if (!open) {
      return;
    }

    const paper = internalPaperRef.current;

    if (!paper) {
      return;
    }

    const anchorRect = anchorEl?.getBoundingClientRect();
    const anchorBox = anchorRect
      ? anchorRect
      : anchorPosition
        ? ({
            top: anchorPosition.top,
            left: anchorPosition.left,
            right: anchorPosition.left,
            bottom: anchorPosition.top,
            width: 0,
            height: 0,
          } as DOMRect)
        : null;

    if (!anchorBox) {
      return;
    }

    const paperRect = paper.getBoundingClientRect();
    const anchorLeft =
      anchorBox.left +
      getHorizontalOffset(anchorOrigin.horizontal, anchorBox.width);
    const anchorTop =
      anchorBox.top +
      getVerticalOffset(anchorOrigin.vertical, anchorBox.height);

    let nextLeft =
      anchorLeft -
      getHorizontalOffset(transformOrigin.horizontal, paperRect.width);
    let nextTop =
      anchorTop - getVerticalOffset(transformOrigin.vertical, paperRect.height);

    nextLeft = Math.min(
      Math.max(nextLeft, VIEWPORT_MARGIN),
      window.innerWidth - paperRect.width - VIEWPORT_MARGIN,
    );
    nextTop = Math.min(
      Math.max(nextTop, VIEWPORT_MARGIN),
      window.innerHeight - paperRect.height - VIEWPORT_MARGIN,
    );

    setPosition({ top: nextTop, left: nextLeft });
  }, [anchorEl, anchorOrigin, anchorPosition, open, transformOrigin]);

  const handleReposition = useEffectEvent(() => {
    updatePosition();
  });

  const handleDismissPointer = useEffectEvent(
    (event: MouseEvent | TouchEvent) => {
      const target = event.target;

      if (!(target instanceof Node)) {
        return;
      }

      if (internalPaperRef.current?.contains(target)) {
        return;
      }

      if (anchorEl?.contains(target)) {
        return;
      }

      onClose?.();
    },
  );

  const handleDismissContextMenu = useEffectEvent((event: MouseEvent) => {
    const target = event.target;

    if (!(target instanceof Node)) {
      return;
    }

    if (internalPaperRef.current?.contains(target)) {
      return;
    }

    if (anchorEl?.contains(target)) {
      return;
    }

    if ((target as HTMLElement).closest?.("[data-allow-context-menu='true']")) {
      return;
    }

    onClose?.();
  });

  const handleDismissKeyDown = useEffectEvent((event: KeyboardEvent) => {
    if (event.key === "Escape") {
      onClose?.();
    }
  });

  useLayoutEffect(() => {
    if (!open) {
      return undefined;
    }

    handleReposition();
    const rafId = window.requestAnimationFrame(handleReposition);
    return () => window.cancelAnimationFrame(rafId);
  }, [open]);

  useEffect(() => {
    if (!open) {
      return undefined;
    }

    window.addEventListener("resize", handleReposition);
    window.addEventListener("scroll", handleReposition, true);

    return () => {
      window.removeEventListener("resize", handleReposition);
      window.removeEventListener("scroll", handleReposition, true);
    };
  }, [open]);

  useEffect(() => {
    if (!open) {
      return undefined;
    }

    document.addEventListener("mousedown", handleDismissPointer);
    document.addEventListener("touchstart", handleDismissPointer);
    document.addEventListener("contextmenu", handleDismissContextMenu);
    document.addEventListener("keydown", handleDismissKeyDown);

    return () => {
      document.removeEventListener("mousedown", handleDismissPointer);
      document.removeEventListener("touchstart", handleDismissPointer);
      document.removeEventListener("contextmenu", handleDismissContextMenu);
      document.removeEventListener("keydown", handleDismissKeyDown);
    };
  }, [open]);

  if (!open) {
    return null;
  }

  const anchorWidth =
    matchAnchorWidth && anchorEl
      ? anchorEl.getBoundingClientRect().width
      : null;

  return createPortal(
    <div
      className={`app-popover-root ${className || ""}`.trim()}
      style={{ zIndex, ...style }}
    >
      <div
        ref={setPaperRef}
        className={`app-popover__paper ${paperClassName || ""}`.trim()}
        style={{
          top: position.top,
          left: position.left,
          width: anchorWidth ? `${anchorWidth}px` : undefined,
          ...paperStyle,
        }}
      >
        {children}
      </div>
    </div>,
    document.body,
  );
};

AppPopover.displayName = "AppPopover";

export default AppPopover;
