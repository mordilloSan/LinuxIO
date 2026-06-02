import { Icon } from "@iconify/react";
import React from "react";

import "./tab-selector.css";

import AppIconButton from "@/components/ui/AppIconButton";
import AppMenu from "@/components/ui/AppMenu";
import { useAppMediaQuery, useAppTheme } from "@/theme";

interface TabOption {
  label: string;
  value: string;
}
interface TabSelectorProps {
  className?: string;
  onChange: (value: string) => void;
  options: TabOption[];
  rightContent?: React.ReactNode;
  style?: React.CSSProperties;
  value: string;
}

const TabSelector: React.FC<TabSelectorProps> = ({
  value,
  onChange,
  options,
  rightContent,
  className,
  style,
}) => {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const [anchorEl, setAnchorEl] = React.useState<HTMLElement | null>(null);

  const primaryHex = theme.palette.primary.main;
  const contrast = theme.palette.primary.contrastText;

  return (
    <div
      className={["tab-selector", className].filter(Boolean).join(" ")}
      style={
        {
          "--tab-selector-active-bg": primaryHex,
          "--tab-selector-active-color": contrast,
          "--tab-selector-border": theme.palette.divider,
          "--tab-selector-hover": theme.palette.action.hover,
          "--tab-selector-text": theme.palette.text.secondary,
          ...style,
        } as React.CSSProperties
      }
    >
      <div className="tab-selector__scroller custom-scrollbar">
        <div aria-label="Tabs" className="tab-selector__pills" role="tablist">
          {options.map((opt) => (
            <button
              aria-selected={value === opt.value}
              className={`tab-selector__pill ${value === opt.value ? "tab-selector__pill--active" : ""}`.trim()}
              key={opt.value}
              onClick={() => onChange(opt.value)}
              role="tab"
              type="button"
            >
              {opt.label}
            </button>
          ))}
        </div>
      </div>

      {rightContent && (
        <>
          {isMobile ? (
            <>
              <AppIconButton
                onClick={(e) => setAnchorEl(e.currentTarget)}
                size="small"
                style={{ marginTop: 2, flexShrink: 0 }}
              >
                <Icon height={20} icon="mdi:tune" width={20} />
              </AppIconButton>
              <AppMenu
                anchorEl={anchorEl}
                anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
                minWidth="unset"
                onClose={() => setAnchorEl(null)}
                open={Boolean(anchorEl)}
                transformOrigin={{ vertical: "top", horizontal: "right" }}
              >
                <div className="tab-selector__mobile-actions">
                  {rightContent}
                </div>
              </AppMenu>
            </>
          ) : (
            <div className="tab-selector__actions">{rightContent}</div>
          )}
        </>
      )}
    </div>
  );
};

export default TabSelector;
