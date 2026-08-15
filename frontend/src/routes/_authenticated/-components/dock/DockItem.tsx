import { Icon } from "@iconify/react";
import { Link } from "@tanstack/react-router";
import { memo } from "react";

import DockTile from "./DockTile";
import type { SidebarItem } from "../sidebar/types";

type DockItemProps = SidebarItem & {
  disabled?: boolean;
  gradient: readonly [string, string];
};

const DockItem = memo<DockItemProps>(
  ({ to, title, icon, params, disabled = false, gradient }) => {
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

    if (disabled) {
      return (
        <li className="app-dock__item">
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
      <li className="app-dock__item">
        <Link
          activeOptions={{ exact: to === "/" }}
          activeProps={{ className: "app-dock-link app-dock-link--active" }}
          className="app-dock-link"
          params={params}
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
