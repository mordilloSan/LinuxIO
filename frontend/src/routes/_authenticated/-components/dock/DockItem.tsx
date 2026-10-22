import { Icon } from "@iconify/react";
import { Link } from "@tanstack/react-router";
import { memo } from "react";

import DockTile from "./DockTile";
import type { SidebarItem } from "../sidebar/types";
import { useSortableNavItem } from "../sidebar/useSortableNavItem";

type DockItemProps = SidebarItem & {
  disabled?: boolean;
  /** Layout mode is open: the tile is being rearranged, not followed. */
  editMode?: boolean;
  gradient: readonly [string, string];
  /** This tile is being held, waiting for the hold to complete. */
  pending?: boolean;
};

const DockItem = memo<DockItemProps>(
  ({
    to,
    title,
    icon,
    params,
    disabled = false,
    editMode = false,
    gradient,
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

    const content = (
      <DockTile gradient={gradient} label={title}>
        {renderIcon()}
      </DockTile>
    );

    const itemClassName = pending
      ? "app-dock__item app-dock__item--reorder-pending"
      : "app-dock__item";

    if (disabled) {
      return (
        <li className={itemClassName} {...itemProps}>
          <span
            aria-disabled="true"
            className="app-dock-link app-dock-link--disabled"
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
          activeProps={{ className: "app-dock-link app-dock-link--active" }}
          aria-describedby={describedBy}
          className="app-dock-link"
          disabled={editMode}
          params={params}
          tabIndex={editMode ? 0 : undefined}
          to={to}
        >
          {content}
        </Link>
      </li>
    );
  },
);

DockItem.displayName = "DockItem";

export default DockItem;
