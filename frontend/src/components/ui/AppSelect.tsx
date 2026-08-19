import {
  Children,
  forwardRef,
  Fragment,
  isValidElement,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
  type OptionHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
} from "react";
import { createPortal } from "react-dom";

import "./app-select.css";

interface AppSelectProps extends Omit<
  SelectHTMLAttributes<HTMLSelectElement>,
  "size"
> {
  disableUnderline?: boolean;
  fullWidth?: boolean;
  label?: string;
  renderOption?: (value: string, label: string) => ReactNode;
  renderValue?: (value: string, label: string) => ReactNode;
  size?: "small" | "medium";
  variant?: "outlined" | "standard";
}

interface OptionData {
  disabled?: boolean;
  hidden?: boolean;
  label: string;
  value: string;
}

// `<option>` children are text in practice, but ReactNode also admits elements,
// which stringify to "[object Object]". Pull out the text parts instead.
function optionLabel(children: ReactNode): string {
  if (typeof children === "string") return children;
  if (typeof children === "number") return String(children);
  if (Array.isArray(children)) return children.map(optionLabel).join("");
  return "";
}

function collectOptions(children: ReactNode): OptionData[] {
  const opts: OptionData[] = [];
  Children.forEach(children, (child) => {
    if (!isValidElement(child)) return;
    if (child.type === Fragment) {
      opts.push(
        ...collectOptions((child.props as { children: ReactNode }).children),
      );
      return;
    }
    if (child.type === "option") {
      const p = child.props as OptionHTMLAttributes<HTMLOptionElement>;
      opts.push({
        value: String(p.value ?? ""),
        label: optionLabel(p.children),
        disabled: !!p.disabled,
        hidden: !!p.hidden,
      });
    }
  });
  return opts;
}

const AppSelect = forwardRef<HTMLDivElement, AppSelectProps>(
  (
    {
      size = "medium",
      variant = "outlined",
      disableUnderline,
      fullWidth,
      label,
      renderOption,
      renderValue,
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
    const dropdownId = useId();
    const labelId = useId();
    const containerRef = useRef<HTMLDivElement>(null);
    const dropdownRef = useRef<HTMLUListElement>(null);
    const [dropdownPos, setDropdownPos] = useState<{
      top: number;
      left: number;
      minWidth: number;
      fontSize: string;
    } | null>(null);

    const options = collectOptions(children);
    const currentValue = String(value ?? "");
    const current = options.find((o) => o.value === currentValue);

    const updatePosition = useCallback(() => {
      if (!containerRef.current) return;
      const rect = containerRef.current.getBoundingClientRect();
      const trigger =
        containerRef.current.querySelector<HTMLElement>(".app-select__input");
      const cs = getComputedStyle(trigger ?? containerRef.current);
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

    // The portaled listbox is position:fixed, so its open-time coordinates stop
    // describing the trigger the moment anything scrolls: the list stays parked
    // over whatever content has slid under it. Capture phase is required —
    // scrolling an element does not bubble. Mirrors AppPopover.
    useEffect(() => {
      if (!open) return undefined;
      window.addEventListener("resize", updatePosition);
      window.addEventListener("scroll", updatePosition, true);
      return () => {
        window.removeEventListener("resize", updatePosition);
        window.removeEventListener("scroll", updatePosition, true);
      };
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
        } as ChangeEvent<HTMLSelectElement>);
      }
    };

    const onKeyDown = (e: KeyboardEvent) => {
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
      <div className={wrapperClass} ref={ref} style={style}>
        {label && (
          <label className="app-select__label" id={labelId}>
            {label}
          </label>
        )}
        <div className="app-select__control" ref={containerRef}>
          <div
            aria-controls={open ? dropdownId : undefined}
            aria-disabled={disabled || undefined}
            aria-expanded={open}
            aria-haspopup="listbox"
            aria-labelledby={label ? labelId : undefined}
            className={triggerClass}
            onClick={toggle}
            onKeyDown={onKeyDown}
            role="combobox"
            tabIndex={disabled ? -1 : 0}
          >
            {current
              ? renderValue
                ? renderValue(current.value, current.label)
                : current.label
              : ""}
          </div>
          <span
            aria-hidden="true"
            className={`app-select__arrow${open ? " app-select__arrow--open" : ""}`}
          >
            <svg height="1.25em" viewBox="0 0 24 24" width="1.25em">
              <path d="M7 10l5 5 5-5z" fill="currentColor" />
            </svg>
          </span>
        </div>
        {open &&
          dropdownPos &&
          createPortal(
            <ul
              id={dropdownId}
              className="app-select__dropdown app-select__dropdown--portal custom-scrollbar"
              ref={dropdownRef}
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
                    aria-selected={opt.value === currentValue}
                    className={[
                      "app-select__option",
                      opt.value === currentValue &&
                        "app-select__option--selected",
                      opt.disabled && "app-select__option--disabled",
                    ]
                      .filter(Boolean)
                      .join(" ")}
                    key={opt.value}
                    onClick={() => select(opt)}
                    onMouseDown={(e) => e.preventDefault()}
                    role="option"
                  >
                    {renderOption
                      ? renderOption(opt.value, opt.label)
                      : opt.label}
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
