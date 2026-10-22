import { Icon } from "@iconify/react";
import { Link } from "@tanstack/react-router";
import { memo } from "react";

import type { SidebarItem } from "./types";
import { useSortableNavItem } from "./useSortableNavItem";

type SidebarNavListItemProps = SidebarItem & {
  collapsed?: boolean;
  disabled?: boolean;
  /** Layout mode is open: the entry is being rearranged, not followed. */
  editMode?: boolean;
  /** This entry is being held, waiting for the hold to complete. */
  pending?: boolean;
};

const SidebarNavList = memo<SidebarNavListItemProps>(
  ({
    to,
    title,
    icon,
    params,
    collapsed = false,
    disabled = false,
    editMode = false,
    pending = false,
  }) => {
    const { describedBy, itemProps } = useSortableNavItem(to, disabled);

    const renderIcon = () => {
      if (!icon) return null;
      if (typeof icon === "string")
        return <Icon height={24} icon={icon} width={24} />;
      const IconComponent = icon;
      return <IconComponent />;
    };

    const baseClassName = [
      "app-sidebar-link",
      collapsed && "app-sidebar-link--collapsed",
      disabled && "app-sidebar-link--disabled",
    ]
      .filter(Boolean)
      .join(" ");
    const itemClassName = pending
      ? "app-sidebar__item app-sidebar__item--reorder-pending"
      : "app-sidebar__item";

    const content = (
      <>
        {icon && <span className="app-sidebar-link__icon">{renderIcon()}</span>}
        <span className="app-sidebar-link__label">
          <span className="app-sidebar-link__label-inner">{title}</span>
        </span>
      </>
    );

    if (disabled) {
      return (
        <li className={itemClassName} {...itemProps}>
          <span
            aria-disabled="true"
            className={baseClassName}
            title={collapsed ? title : undefined}
          >
            {content}
          </span>
        </li>
      );
    }

    return (
      <li className={itemClassName} {...itemProps}>
        {/* Layout mode drops the href: dnd-kit only stops the trailing click
            from propagating, so a live href would still navigate natively.
            tabIndex keeps the bare anchor focusable through a keyboard drag. */}
        <Link
          activeOptions={{ exact: to === "/" }}
          activeProps={{
            className: `${baseClassName} app-sidebar-link--active`,
          }}
          aria-describedby={describedBy}
          className={baseClassName}
          disabled={editMode}
          params={params}
          tabIndex={editMode ? 0 : undefined}
          title={collapsed ? title : undefined}
          to={to}
        >
          {content}
        </Link>
      </li>
    );
  },
);

SidebarNavList.displayName = "SidebarNavList";

export default SidebarNavList;
