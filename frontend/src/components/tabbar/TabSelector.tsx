import type { CSSProperties } from "react";

import "./tab-selector.css";

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

// Shared by TabSelector and RoutedTabContainer's own tab strip so the two
// implementations of the same `tab-selector` class can never drift apart.
export const getTabSelectorThemeVars = (): CSSProperties =>
  ({
    "--tab-selector-active-bg": "var(--app-palette-primary-main)",
    "--tab-selector-active-color": "var(--app-palette-primary-contrast-text)",
    "--tab-selector-hover": "var(--app-palette-action-hover)",
    "--tab-selector-text": "var(--app-palette-text-secondary)",
  }) as CSSProperties;

const TabSelector = <TValue extends string>({
  value,
  onChange,
  options,
  className,
  style,
}: TabSelectorProps<TValue>) => {
  return (
    <div
      className={["tab-selector", className].filter(Boolean).join(" ")}
      style={{
        ...getTabSelectorThemeVars(),
        ...style,
      }}
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
