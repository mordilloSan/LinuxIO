import React, { useEffect, useRef } from "react";

import AppPopover, { AppPopoverOrigin } from "./AppPopover";
import "./app-menu.css";

export interface AppMenuProps {
  anchorEl?: HTMLElement | null;
  anchorOrigin?: AppPopoverOrigin;
  anchorPosition?: { top: number; left: number } | null;
  autoFocus?: boolean;
  children: React.ReactNode;
  className?: string;
  minWidth?: number | string;
  onClose: () => void;
  open: boolean;
  style?: React.CSSProperties;
  transformOrigin?: AppPopoverOrigin;
}

export interface AppMenuItemProps extends Omit<
  React.ButtonHTMLAttributes<HTMLButtonElement>,
  "color"
> {
  endAdornment?: React.ReactNode;
  selected?: boolean;
  startAdornment?: React.ReactNode;
}

const focusableSelector = '[role="menuitem"]:not(:disabled)';

const AppMenu: React.FC<AppMenuProps> = ({
  open,
  onClose,
  anchorEl,
  anchorPosition,
  anchorOrigin,
  transformOrigin,
  autoFocus = true,
  minWidth,
  children,
  className,
  style,
}) => {
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open || !autoFocus) {
      return;
    }

    const firstItem =
      menuRef.current?.querySelector<HTMLButtonElement>(focusableSelector);
    firstItem?.focus();
  }, [autoFocus, open]);

  const focusRelativeItem = (direction: 1 | -1) => {
    const items = Array.from(
      menuRef.current?.querySelectorAll<HTMLButtonElement>(focusableSelector) ??
        [],
    );

    if (!items.length) {
      return;
    }

    const currentIndex = items.findIndex(
      (item) => item === document.activeElement,
    );
    const baseIndex =
      currentIndex === -1 ? (direction === 1 ? -1 : 0) : currentIndex;
    const nextIndex = (baseIndex + direction + items.length) % items.length;

    items[nextIndex]?.focus();
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        focusRelativeItem(1);
        break;
      case "ArrowUp":
        event.preventDefault();
        focusRelativeItem(-1);
        break;
      case "Home": {
        event.preventDefault();
        const firstItem =
          menuRef.current?.querySelector<HTMLButtonElement>(focusableSelector);
        firstItem?.focus();
        break;
      }
      case "End": {
        event.preventDefault();
        const items = Array.from(
          menuRef.current?.querySelectorAll<HTMLButtonElement>(
            focusableSelector,
          ) ?? [],
        );
        items.at(-1)?.focus();
        break;
      }
      case "Tab":
        onClose();
        break;
      default:
        break;
    }
  };

  return (
    <AppPopover
      anchorEl={anchorEl}
      anchorOrigin={anchorOrigin}
      anchorPosition={anchorPosition}
      onClose={onClose}
      open={open}
      paperClassName={`app-menu ${className || ""}`.trim()}
      paperStyle={{
        minWidth,
        ...style,
      }}
      transformOrigin={transformOrigin}
    >
      <div
        className="app-menu__content"
        onKeyDown={handleKeyDown}
        ref={menuRef}
        role="menu"
      >
        {children}
      </div>
    </AppPopover>
  );
};

export const AppMenuItem = React.forwardRef<
  HTMLButtonElement,
  AppMenuItemProps
>(
  (
    {
      selected = false,
      startAdornment,
      endAdornment,
      disabled,
      className,
      children,
      ...rest
    },
    ref,
  ) => (
    <button
      className={[
        "app-menu__item",
        selected && "app-menu__item--selected",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
      disabled={disabled}
      ref={ref}
      role="menuitem"
      type="button"
      {...rest}
    >
      {startAdornment ? (
        <span className="app-menu__item-start">{startAdornment}</span>
      ) : null}
      <span className="app-menu__item-label">{children}</span>
      {endAdornment ? (
        <span className="app-menu__item-end">{endAdornment}</span>
      ) : null}
    </button>
  ),
);

AppMenu.displayName = "AppMenu";
AppMenuItem.displayName = "AppMenuItem";

export default AppMenu;
