import { Icon } from "@iconify/react";
import React, { useEffect, useId, useMemo, useRef, useState } from "react";

import AppPopover from "./AppPopover";
import AppTextField from "./AppTextField";

import "./app-autocomplete.css";

interface FilterState {
  inputValue: string;
}

interface BaseAutocompleteProps {
  options: string[];
  label?: string;
  placeholder?: string;
  size?: "small" | "medium";
  fullWidth?: boolean;
  disabled?: boolean;
  helperText?: React.ReactNode;
  style?: React.CSSProperties;
  className?: string;
  freeSolo?: boolean;
  loading?: boolean;
  noOptionsText?: React.ReactNode;
  shrinkLabel?: boolean;
  autoFocus?: boolean;
  endAdornment?: React.ReactNode;
  filterOptions?: (options: string[], state: FilterState) => string[];
  onInputChange?: (value: string) => void;
}

type RenderValueProps = (
  value: string[],
  getItemProps: ({ index }: { index: number }) => {
    key: string;
    onDelete: () => void;
  },
) => React.ReactNode;

type SingleAutocompleteProps = BaseAutocompleteProps & {
  multiple?: false;
  value: string;
  onChange?: (value: string) => void;
};

type MultipleAutocompleteProps = BaseAutocompleteProps & {
  multiple: true;
  value: string[];
  onChange?: (value: string[]) => void;
  renderValue?: RenderValueProps;
};

export type AppAutocompleteProps =
  | SingleAutocompleteProps
  | MultipleAutocompleteProps;

const defaultFilterOptions = (options: string[], state: FilterState) => {
  if (!state.inputValue) {
    return options;
  }

  const lowerValue = state.inputValue.toLowerCase();
  return options.filter((option) => option.toLowerCase().includes(lowerValue));
};

const AppAutocomplete: React.FC<AppAutocompleteProps> = (props) => {
  const {
    options,
    label,
    placeholder,
    size = "medium",
    fullWidth,
    disabled = false,
    helperText,
    style,
    className,
    freeSolo = false,
    loading = false,
    noOptionsText = "No matches",
    shrinkLabel,
    autoFocus,
    endAdornment,
    filterOptions = defaultFilterOptions,
    onInputChange,
  } = props;

  const isMultiple = props.multiple === true;
  const listboxId = useId();
  const containerRef = useRef<HTMLDivElement>(null);
  const listboxRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const [singleInputValue, setSingleInputValue] = useState(
    isMultiple ? "" : props.value,
  );
  const [multipleInputValue, setMultipleInputValue] = useState("");

  const selectedValues = isMultiple ? props.value : [];
  const inputValue = isMultiple ? multipleInputValue : singleInputValue;

  useEffect(() => {
    if (!isMultiple) {
      setSingleInputValue(props.value);
    }
  }, [isMultiple, props.value]);

  const filteredOptions = useMemo(() => {
    const uniqueOptions = Array.from(new Set(options));
    const availableOptions = isMultiple
      ? uniqueOptions.filter((option) => !selectedValues.includes(option))
      : uniqueOptions;

    return filterOptions(availableOptions, { inputValue });
  }, [filterOptions, inputValue, isMultiple, options, selectedValues]);

  useEffect(() => {
    if (!open) {
      setActiveIndex(-1);
      return;
    }

    if (!filteredOptions.length) {
      setActiveIndex(-1);
      return;
    }

    setActiveIndex((current) =>
      current >= 0 && current < filteredOptions.length ? current : 0,
    );
  }, [filteredOptions.length, open]);

  const updateInputValue = (nextValue: string) => {
    if (isMultiple) {
      setMultipleInputValue(nextValue);
    } else {
      setSingleInputValue(nextValue);
    }

    onInputChange?.(nextValue);
  };

  const handleSelect = (nextValue: string) => {
    if (isMultiple) {
      const nextSelection = [...selectedValues, nextValue];
      props.onChange?.(nextSelection);
      setMultipleInputValue("");
      onInputChange?.("");
      setOpen(true);
      setActiveIndex(-1);
      inputRef.current?.focus();
      return;
    }

    setSingleInputValue(nextValue);
    onInputChange?.(nextValue);
    props.onChange?.(nextValue);
    setOpen(false);
  };

  const removeSelectedValue = (index: number) => {
    if (!isMultiple) {
      return;
    }

    const nextSelection = selectedValues.filter(
      (_, itemIndex) => itemIndex !== index,
    );
    props.onChange?.(nextSelection);
    inputRef.current?.focus();
  };

  const commitFreeSoloValue = () => {
    const nextValue = inputValue.trim();

    if (!nextValue) {
      if (!isMultiple) {
        props.onChange?.("");
      }
      setOpen(false);
      return;
    }

    handleSelect(nextValue);
  };

  const handleInputBlur = () => {
    window.requestAnimationFrame(() => {
      const activeElement = document.activeElement;

      if (
        activeElement instanceof Node &&
        (containerRef.current?.contains(activeElement) ||
          listboxRef.current?.contains(activeElement))
      ) {
        return;
      }

      setOpen(false);
    });
  };

  const handleInputKeyDown = (event: React.KeyboardEvent<Element>) => {
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        setOpen(true);
        setActiveIndex((current) =>
          filteredOptions.length
            ? current < filteredOptions.length - 1
              ? current + 1
              : 0
            : -1,
        );
        break;
      case "ArrowUp":
        event.preventDefault();
        setOpen(true);
        setActiveIndex((current) =>
          filteredOptions.length
            ? current > 0
              ? current - 1
              : filteredOptions.length - 1
            : -1,
        );
        break;
      case "Enter":
        if (open && activeIndex >= 0 && filteredOptions[activeIndex]) {
          event.preventDefault();
          handleSelect(filteredOptions[activeIndex]);
          break;
        }

        if (freeSolo) {
          event.preventDefault();
          commitFreeSoloValue();
        }
        break;
      case "Escape":
        setOpen(false);
        break;
      case "Backspace":
        if (isMultiple && !inputValue && selectedValues.length > 0) {
          removeSelectedValue(selectedValues.length - 1);
        }
        break;
      default:
        break;
    }
  };

  const renderedValue = isMultiple
    ? (props.renderValue?.(selectedValues, ({ index }: { index: number }) => ({
        key: selectedValues[index] ?? String(index),
        onDelete: () => removeSelectedValue(index),
      })) ??
      selectedValues.map((option, index) => (
        <span key={`${option}-${index}`} className="app-autocomplete__tag">
          <span className="app-autocomplete__tag-label">{option}</span>
          <button
            type="button"
            className="app-autocomplete__tag-remove"
            onClick={() => removeSelectedValue(index)}
            aria-label={`Remove ${option}`}
          >
            <Icon icon="mdi:close-circle" width={16} height={16} />
          </button>
        </span>
      )))
    : null;

  return (
    <div
      ref={containerRef}
      className={[
        "app-autocomplete",
        fullWidth && "app-autocomplete--fullwidth",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
      style={style}
    >
      {isMultiple && selectedValues.length > 0 ? (
        <div className="app-autocomplete__tags">{renderedValue}</div>
      ) : null}

      <AppTextField
        ref={inputRef}
        label={label}
        value={inputValue}
        placeholder={placeholder}
        size={size}
        fullWidth={fullWidth}
        disabled={disabled}
        helperText={helperText}
        shrinkLabel={shrinkLabel || (isMultiple && selectedValues.length > 0)}
        autoFocus={autoFocus}
        endAdornment={
          <div className="app-autocomplete__end">
            {endAdornment}
            <Icon icon="mdi:chevron-down" width={18} height={18} />
          </div>
        }
        onFocus={() => setOpen(true)}
        onBlur={handleInputBlur}
        onClick={() => setOpen(true)}
        onKeyDown={handleInputKeyDown}
        onChange={(event) => {
          updateInputValue(event.target.value);
          setOpen(true);
        }}
        role="combobox"
        aria-expanded={open}
        aria-controls={open ? listboxId : undefined}
        aria-autocomplete="list"
      />

      <AppPopover
        open={open && !disabled && (loading || filteredOptions.length > 0)}
        onClose={() => setOpen(false)}
        anchorEl={containerRef.current}
        anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
        transformOrigin={{ vertical: "top", horizontal: "left" }}
        matchAnchorWidth
        paperClassName="app-autocomplete__panel"
      >
        <div
          ref={listboxRef}
          id={listboxId}
          className="app-autocomplete__listbox custom-scrollbar"
          role="listbox"
        >
          {loading ? (
            <div className="app-autocomplete__status">Loading…</div>
          ) : filteredOptions.length > 0 ? (
            filteredOptions.map((option, index) => (
              <button
                key={option}
                type="button"
                role="option"
                aria-selected={index === activeIndex}
                className={[
                  "app-autocomplete__option",
                  index === activeIndex && "app-autocomplete__option--active",
                ]
                  .filter(Boolean)
                  .join(" ")}
                onMouseDown={(event) => event.preventDefault()}
                onMouseEnter={() => setActiveIndex(index)}
                onClick={() => handleSelect(option)}
              >
                {option}
              </button>
            ))
          ) : (
            <div className="app-autocomplete__status">{noOptionsText}</div>
          )}
        </div>
      </AppPopover>
    </div>
  );
};

AppAutocomplete.displayName = "AppAutocomplete";

export default AppAutocomplete;
