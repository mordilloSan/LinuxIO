import React, { useState, useRef, useEffect, useLayoutEffect, useCallback } from "react";
import { createPortal } from "react-dom";

import "./app-select.css";

// Frozen compatibility wrapper: do not introduce new usages.

interface AppSelectProps extends Omit<
  React.SelectHTMLAttributes<HTMLSelectElement>,
  "size"
> {
  size?: "small" | "medium";
  variant?: "outlined" | "standard";
  disableUnderline?: boolean;
  fullWidth?: boolean;
  label?: string;
}

interface OptionData {
  value: string;
  label: string;
  disabled?: boolean;
  hidden?: boolean;
}

function collectOptions(children: React.ReactNode): OptionData[] {
  const opts: OptionData[] = [];
  React.Children.forEach(children, (child) => {
    if (!React.isValidElement(child)) return;
    if (child.type === React.Fragment) {
      opts.push(
        ...collectOptions(
          (child.props as { children: React.ReactNode }).children,
        ),
      );
      return;
    }
    if (child.type === "option") {
      const p = child.props as React.OptionHTMLAttributes<HTMLOptionElement>;
      opts.push({
        value: String(p.value ?? ""),
        label: String(p.children ?? ""),
        disabled: !!p.disabled,
        hidden: !!p.hidden,
      });
    }
  });
  return opts;
}

const AppSelect = React.forwardRef<HTMLDivElement, AppSelectProps>(
  (
    {
      size = "medium",
      variant = "outlined",
      disableUnderline,
      fullWidth,
      label,
      className,
      style,
      children,
      value,
      onChange,
      disabled,
    },
    ref,
  ) => {
    const [open, setOpen] = useState(false);
    const containerRef = useRef<HTMLDivElement>(null);
    const dropdownRef = useRef<HTMLUListElement>(null);
    const [dropdownPos, setDropdownPos] = useState<{ top: number; left: number; minWidth: number; fontSize: string } | null>(null);

    const options = collectOptions(children);
    const currentValue = String(value ?? "");
    const current = options.find((o) => o.value === currentValue);

    const updatePosition = useCallback(() => {
      if (!containerRef.current) return;
      const rect = containerRef.current.getBoundingClientRect();
      const cs = getComputedStyle(containerRef.current);
      setDropdownPos({
        top: rect.bottom + 2,
        left: rect.left,
        minWidth: rect.width,
        fontSize: cs.fontSize,
      });
    }, []);

    useLayoutEffect(() => {
      if (open) updatePosition();
    }, [open, updatePosition]);

    useEffect(() => {
      if (!open) return;
      const handler = (e: MouseEvent) => {
        if (
          containerRef.current &&
          !containerRef.current.contains(e.target as Node) &&
          dropdownRef.current &&
          !dropdownRef.current.contains(e.target as Node)
        ) {
          setOpen(false);
        }
      };
      document.addEventListener("mousedown", handler);
      return () => document.removeEventListener("mousedown", handler);
    }, [open]);

    const toggle = () => {
      if (!disabled) setOpen((v) => !v);
    };

    const select = (opt: OptionData) => {
      if (opt.disabled) return;
      setOpen(false);
      if (onChange) {
        onChange({
          target: { value: opt.value },
        } as React.ChangeEvent<HTMLSelectElement>);
      }
    };

    const onKeyDown = (e: React.KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        return;
      }
      if (e.key === " " || e.key === "Enter") {
        e.preventDefault();
        toggle();
        return;
      }
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const selectable = options.filter((o) => !o.disabled && !o.hidden);
        const idx = selectable.findIndex((o) => o.value === currentValue);
        const next =
          e.key === "ArrowDown"
            ? selectable[Math.min(idx + 1, selectable.length - 1)]
            : selectable[Math.max(idx - 1, 0)];
        if (next) select(next);
      }
    };

    const wrapperClass = [
      "app-select",
      size === "small" && "app-select--small",
      variant === "standard" && "app-select--standard",
      disableUnderline && "app-select--no-underline",
      fullWidth && "app-select--full-width",
    ]
      .filter(Boolean)
      .join(" ");

    const triggerClass = [
      "app-select__input",
      disabled && "app-select__input--disabled",
      open && "app-select__input--open",
      className,
    ]
      .filter(Boolean)
      .join(" ");

    return (
      <div className={wrapperClass} style={style} ref={ref}>
        {label && <label className="app-select__label">{label}</label>}
        <div className="app-select__control" ref={containerRef}>
          <div
            className={triggerClass}
            role="combobox"
            aria-expanded={open}
            aria-haspopup="listbox"
            tabIndex={disabled ? -1 : 0}
            onClick={toggle}
            onKeyDown={onKeyDown}
          >
            {current?.label ?? ""}
          </div>
          <span
            className={`app-select__arrow${open ? " app-select__arrow--open" : ""}`}
            aria-hidden="true"
          >
            <svg viewBox="0 0 24 24" width="1.25em" height="1.25em">
              <path d="M7 10l5 5 5-5z" fill="currentColor" />
            </svg>
          </span>
        </div>
        {open && dropdownPos && createPortal(
          <ul
            ref={dropdownRef}
            className="app-select__dropdown app-select__dropdown--portal"
            role="listbox"
            style={{
              top: dropdownPos.top,
              left: dropdownPos.left,
              minWidth: dropdownPos.minWidth,
              fontSize: dropdownPos.fontSize,
            }}
          >
            {options
              .filter((o) => !o.hidden)
              .map((opt) => (
                <li
                  key={opt.value}
                  role="option"
                  aria-selected={opt.value === currentValue}
                  className={[
                    "app-select__option",
                    opt.value === currentValue &&
                      "app-select__option--selected",
                    opt.disabled && "app-select__option--disabled",
                  ]
                    .filter(Boolean)
                    .join(" ")}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => select(opt)}
                >
                  {opt.label}
                </li>
              ))}
          </ul>,
          document.body,
        )}
      </div>
    );
  },
);

AppSelect.displayName = "AppSelect";

export default AppSelect;
