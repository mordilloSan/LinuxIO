import type { CSSProperties } from "react";

import "./tab-selector.css";

import { useAppTheme } from "@/theme";

interface TabOption<TValue extends string> {
  label: string;
  value: TValue;
}
interface TabSelectorProps<TValue extends string> {
  className?: string;
  onChange: (value: TValue) => void;
  options: readonly TabOption<TValue>[];
  style?: CSSProperties;
  value: TValue;
}

const TabSelector = <TValue extends string>({
  value,
  onChange,
  options,
  className,
  style,
}: TabSelectorProps<TValue>) => {
  const theme = useAppTheme();

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
        } as CSSProperties
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
    </div>
  );
};

export default TabSelector;
