import { Icon } from "@iconify/react";
import React from "react";
import { NavLink } from "react-router-dom";

interface SidebarNavListItemProps {
  href: string;
  title: string;
  icon?: React.ElementType | string;
  collapsed?: boolean;
  disabled?: boolean;
}

const SidebarNavList: React.FC<SidebarNavListItemProps> = React.memo(
  ({ href, title, icon, collapsed = false, disabled = false }) => {
    const renderIcon = () => {
      if (!icon) return null;
      if (typeof icon === "string")
        return <Icon icon={icon} width={24} height={24} />;
      const IconComponent = icon as React.ElementType;
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
        <span className="app-sidebar-link__label">{title}</span>
      </>
    );

    if (disabled) {
      return (
        <li>
          <span
            className={baseClassName}
            aria-disabled="true"
            title={collapsed ? title : undefined}
          >
            {content}
          </span>
        </li>
      );
    }

    return (
      <li>
        <NavLink
          to={href}
          title={collapsed ? title : undefined}
          className={({ isActive }) =>
            [baseClassName, isActive && "app-sidebar-link--active"]
              .filter(Boolean)
              .join(" ")
          }
        >
          {content}
        </NavLink>
      </li>
    );
  },
);

SidebarNavList.displayName = "SidebarNavList";

export default SidebarNavList;
