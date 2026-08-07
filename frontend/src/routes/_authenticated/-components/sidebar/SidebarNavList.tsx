import { Icon } from "@iconify/react";
import { Link } from "@tanstack/react-router";
import { memo, type ElementType } from "react";

import type { SidebarItem } from "./types";

type SidebarNavListItemProps = SidebarItem & {
  collapsed?: boolean;
  disabled?: boolean;
};

const SidebarNavList = memo<SidebarNavListItemProps>(
  ({ to, title, icon, params, collapsed = false, disabled = false }) => {
    const renderIcon = () => {
      if (!icon) return null;
      if (typeof icon === "string")
        return <Icon height={24} icon={icon} width={24} />;
      const IconComponent = icon as ElementType;
      return <IconComponent />;
    };

    const baseClassName = [
      "app-sidebar-link",
      collapsed && "app-sidebar-link--collapsed",
      disabled && "app-sidebar-link--disabled",
    ]
      .filter(Boolean)
      .join(" ");

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
        <li>
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
      <li>
        <Link
          activeOptions={{ exact: to === "/" }}
          activeProps={{
            className: `${baseClassName} app-sidebar-link--active`,
          }}
          className={baseClassName}
          params={params}
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
