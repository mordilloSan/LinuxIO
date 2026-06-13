import { Icon } from "@iconify/react";
import React from "react";
import { NavLink } from "react-router-dom";

interface SidebarNavListItemProps {
  collapsed?: boolean;
  disabled?: boolean;
  href: string;
  icon?: React.ElementType | string;
  preload?: () => Promise<unknown>;
  title: string;
}

const SidebarNavList: React.FC<SidebarNavListItemProps> = React.memo(
  ({ href, title, icon, preload, collapsed = false, disabled = false }) => {
    const preloadRequestedRef = React.useRef(false);

    React.useEffect(() => {
      preloadRequestedRef.current = false;
    }, [preload]);

    const handlePreloadIntent = React.useCallback(() => {
      if (disabled || !preload || preloadRequestedRef.current) return;

      preloadRequestedRef.current = true;
      void preload().catch(() => {
        preloadRequestedRef.current = false;
      });
    }, [disabled, preload]);

    const renderIcon = () => {
      if (!icon) return null;
      if (typeof icon === "string")
        return <Icon height={24} icon={icon} width={24} />;
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
        <NavLink
          className={({ isActive }) =>
            [baseClassName, isActive && "app-sidebar-link--active"]
              .filter(Boolean)
              .join(" ")
          }
          onFocus={handlePreloadIntent}
          onMouseDown={handlePreloadIntent}
          onPointerEnter={handlePreloadIntent}
          onTouchStart={handlePreloadIntent}
          title={collapsed ? title : undefined}
          to={href}
        >
          {content}
        </NavLink>
      </li>
    );
  },
);

SidebarNavList.displayName = "SidebarNavList";

export default SidebarNavList;
