import { Icon } from "@iconify/react";
import React from "react";

import "./tab-selector.css";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import { AppDialogContent, AppDialogTitle } from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
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
}

const TabSelector: React.FC<TabSelectorProps> = ({
  value,
  onChange,
  options,
  rightContent,
}) => {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const [anchorEl, setAnchorEl] = React.useState<HTMLButtonElement | null>(
    null,
  );

  const primaryHex = theme.palette.primary.main;
  const contrast = theme.palette.primary.contrastText;

  return (
    <div
      className="tab-selector"
      style={{
        "--tab-selector-active-bg": primaryHex,
        "--tab-selector-active-color": contrast,
        "--tab-selector-border": theme.palette.divider,
        "--tab-selector-hover": theme.palette.action.hover,
        "--tab-selector-text": theme.palette.text.secondary,
      }}
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
              <GeneralDialog
                open={Boolean(anchorEl)}
                onClose={() => setAnchorEl(null)}
                maxWidth="xs"
                fullWidth
              >
                <AppDialogTitle>Actions</AppDialogTitle>
                <AppDialogContent>
                  <div className="tab-selector__mobile-actions">
                    {rightContent}
                  </div>
                </AppDialogContent>
              </GeneralDialog>
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
