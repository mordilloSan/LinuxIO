import { Icon } from "@iconify/react";
import React from "react";

import "./tab-selector.css";

import AppIconButton from "@/components/ui/AppIconButton";
import AppMenu from "@/components/ui/AppMenu";
import { useAppMediaQuery, useAppTheme } from "@/theme";

interface TabOption {
  value: string;
  label: string;
}
interface TabSelectorProps {
  value: string;
  onChange: (value: string) => void;
  options: TabOption[];
  rightContent?: React.ReactNode;
  className?: string;
  style?: React.CSSProperties;
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
        <div className="tab-selector__pills" role="tablist" aria-label="Tabs">
          {options.map((opt) => (
            <button
              key={opt.value}
              type="button"
              role="tab"
              aria-selected={value === opt.value}
              className={`tab-selector__pill ${value === opt.value ? "tab-selector__pill--active" : ""}`.trim()}
              onClick={() => onChange(opt.value)}
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
                size="small"
                onClick={(e) => setAnchorEl(e.currentTarget)}
                style={{ marginTop: 2, flexShrink: 0 }}
              >
                <Icon icon="mdi:tune" width={20} height={20} />
              </AppIconButton>
              <AppMenu
                open={Boolean(anchorEl)}
                onClose={() => setAnchorEl(null)}
                anchorEl={anchorEl}
                anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
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
