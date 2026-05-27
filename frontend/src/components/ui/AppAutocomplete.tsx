import { Icon } from "@iconify/react";
import React, { useCallback, useId, useMemo, useRef, useState } from "react";

import AppPopover from "./AppPopover";
import AppTextField from "./AppTextField";
import "./app-autocomplete.css";

interface FilterState {
  inputValue: string;
}

interface BaseAutocompleteProps {
  autoFocus?: boolean;
  className?: string;
  disabled?: boolean;
  endAdornment?: React.ReactNode;
  filterOptions?: (options: string[], state: FilterState) => string[];
  freeSolo?: boolean;
  fullWidth?: boolean;
  helperText?: React.ReactNode;
  label?: string;
  loading?: boolean;
  noOptionsText?: React.ReactNode;
  onInputChange?: (value: string) => void;
  options: string[];
  placeholder?: string;
  shrinkLabel?: boolean;
  size?: "small" | "medium";
  style?: React.CSSProperties;
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
  const [anchorEl, setAnchorEl] = useState<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const [singleInputDraft, setSingleInputDraft] = useState<string | null>(null);
  const [singleInputDraftBaseValue, setSingleInputDraftBaseValue] = useState(
    isMultiple ? "" : props.value,
  );
  const [multipleInputValue, setMultipleInputValue] = useState("");

  const setContainerNode = useCallback((node: HTMLDivElement | null) => {
    containerRef.current = node;
  }, []);

  const setFieldAnchorNode = useCallback((node: HTMLDivElement | null) => {
    setAnchorEl(node);
  }, []);

  const singleValue = props.multiple === true ? "" : props.value;
  const selectedValues = useMemo(
    () => (isMultiple ? props.value : []),
    [isMultiple, props.value],
  );
  const singleInputValue =
    singleInputDraft !== null && singleInputDraftBaseValue === singleValue
      ? singleInputDraft
      : singleValue;
  const inputValue = isMultiple ? multipleInputValue : singleInputValue;

  const filteredOptions = useMemo(() => {
    const uniqueOptions = Array.from(new Set(options));
    const availableOptions = isMultiple
      ? uniqueOptions.filter((option) => !selectedValues.includes(option))
      : uniqueOptions;

    return filterOptions(availableOptions, { inputValue });
  }, [filterOptions, inputValue, isMultiple, options, selectedValues]);
  const resolvedActiveIndex =
    open && filteredOptions.length > 0
      ? activeIndex >= 0 && activeIndex < filteredOptions.length
        ? activeIndex
        : 0
      : -1;

  const updateInputValue = (nextValue: string) => {
    if (isMultiple) {
      setMultipleInputValue(nextValue);
    } else {
      setSingleInputDraft(nextValue);
      setSingleInputDraftBaseValue(singleValue);
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

    setSingleInputDraft(nextValue);
    setSingleInputDraftBaseValue(singleValue);
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
        setSingleInputDraft("");
        setSingleInputDraftBaseValue(singleValue);
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
        setActiveIndex(
          filteredOptions.length
            ? resolvedActiveIndex < filteredOptions.length - 1
              ? resolvedActiveIndex + 1
              : 0
            : -1,
        );
        break;
      case "ArrowUp":
        event.preventDefault();
        setOpen(true);
        setActiveIndex(
          filteredOptions.length
            ? resolvedActiveIndex > 0
              ? resolvedActiveIndex - 1
              : filteredOptions.length - 1
            : -1,
        );
        break;
      case "Enter":
        if (
          open &&
          resolvedActiveIndex >= 0 &&
          filteredOptions[resolvedActiveIndex]
        ) {
          event.preventDefault();
          handleSelect(filteredOptions[resolvedActiveIndex]);
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
        <span className="app-autocomplete__tag" key={`${option}-${index}`}>
          <span className="app-autocomplete__tag-label">{option}</span>
          <button
            aria-label={`Remove ${option}`}
            className="app-autocomplete__tag-remove"
            onClick={() => removeSelectedValue(index)}
            type="button"
          >
            <Icon height={16} icon="mdi:close-circle" width={16} />
          </button>
        </span>
      )))
    : null;

  return (
    <div
      className={[
        "app-autocomplete",
        fullWidth && "app-autocomplete--fullwidth",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
      ref={setContainerNode}
      style={style}
    >
      <div className="app-autocomplete__field" ref={setFieldAnchorNode}>
        <AppTextField
          aria-autocomplete="list"
          aria-controls={open ? listboxId : undefined}
          aria-expanded={open}
          autoFocus={autoFocus}
          disabled={disabled}
          endAdornment={
            <div className="app-autocomplete__end">
              {endAdornment}
              <Icon height={18} icon="mdi:chevron-down" width={18} />
            </div>
          }
          fullWidth={fullWidth}
          helperText={helperText}
          label={label}
          onBlur={handleInputBlur}
          onChange={(event) => {
            updateInputValue(event.target.value);
            setOpen(true);
          }}
          onClick={() => setOpen(true)}
          onFocus={() => setOpen(true)}
          onKeyDown={handleInputKeyDown}
          placeholder={placeholder}
          ref={inputRef}
          role="combobox"
          shrinkLabel={shrinkLabel || (isMultiple && selectedValues.length > 0)}
          size={size}
          value={inputValue}
        />
      </div>

      {isMultiple && selectedValues.length > 0 ? (
        <div className="app-autocomplete__tags">{renderedValue}</div>
      ) : null}

      <AppPopover
        anchorEl={anchorEl}
        anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
        matchAnchorWidth
        onClose={() => setOpen(false)}
        open={open && !disabled && (loading || filteredOptions.length > 0)}
        paperClassName="app-autocomplete__panel"
        transformOrigin={{ vertical: "top", horizontal: "left" }}
      >
        <div
          className="app-autocomplete__listbox custom-scrollbar"
          id={listboxId}
          ref={listboxRef}
          role="listbox"
        >
          {loading ? (
            <div className="app-autocomplete__status">Loading…</div>
          ) : filteredOptions.length > 0 ? (
            filteredOptions.map((option, index) => (
              <button
                aria-selected={index === resolvedActiveIndex}
                className={[
                  "app-autocomplete__option",
                  index === resolvedActiveIndex &&
                    "app-autocomplete__option--active",
                ]
                  .filter(Boolean)
                  .join(" ")}
                key={option}
                onClick={() => handleSelect(option)}
                onMouseDown={(event) => event.preventDefault()}
                onMouseEnter={() => setActiveIndex(index)}
                role="option"
                type="button"
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
