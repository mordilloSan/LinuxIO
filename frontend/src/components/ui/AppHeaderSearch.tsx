import { Icon } from "@iconify/react";
import {
  memo,
  useCallback,
  useEffect,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
} from "react";

import "./app-header-search.css";

import { useAppTheme } from "@/theme";

import { OVERLAY_ROOT_SELECTOR } from "./AppDialog";
import AppIconButton from "./AppIconButton";
import AppTextField from "./AppTextField";

interface AppHeaderSearchProps {
  "aria-label"?: string;
  /**
   * Escape pressed anywhere clears the field even while it isn't focused —
   * without moving focus into it. Left off where the page owns bigger Escape
   * semantics (the file browser's selection model).
   */
  clearOnDocumentEscape?: boolean;
  disabled?: boolean;
  onChange: (value: string) => void;
  placeholder?: string;
  value: string;
}

const AppHeaderSearch = ({
  value,
  onChange,
  placeholder = "Search...",
  disabled = false,
  clearOnDocumentEscape = false,
  "aria-label": ariaLabel,
}: AppHeaderSearchProps) => {
  const theme = useAppTheme();
  const [isFocused, setIsFocused] = useState(false);

  const handleClear = useCallback(() => {
    onChange("");
  }, [onChange]);

  const handleChange = useCallback(
    (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      onChange(event.target.value);
    },
    [onChange],
  );

  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      if (event.key !== "Escape" || value === "") {
        return;
      }
      // Clearing is this press's whole job: mark it handled so whatever hosts
      // the field — the mobile popover, a dialog — stays put, and focus stays
      // in the field for the next query. An empty field lets Escape bubble,
      // which is what closes the host: the combobox cascade.
      event.preventDefault();
      onChange("");
    },
    [onChange, value],
  );

  useEffect(() => {
    if (!clearOnDocumentEscape || value === "") {
      return;
    }

    const handleDocumentKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented) {
        return;
      }
      // An open dialog owns Escape. Its own handler may run after this one
      // (both are document listeners, ordered by registration), so DOM
      // presence is the signal, not defaultPrevented.
      if (document.querySelector(OVERLAY_ROOT_SELECTOR)) {
        return;
      }
      onChange("");
    };

    document.addEventListener("keydown", handleDocumentKeyDown);
    return () => document.removeEventListener("keydown", handleDocumentKeyDown);
  }, [clearOnDocumentEscape, onChange, value]);

  return (
    <AppTextField
      aria-label={ariaLabel ?? placeholder}
      className="app-header-search"
      disabled={disabled}
      endAdornment={
        value ? (
          <AppIconButton
            aria-label="Clear search"
            disabled={disabled}
            edge="end"
            onClick={handleClear}
            size="small"
            style={{ padding: 4 }}
          >
            <Icon height={18} icon="mdi:close" width={18} />
          </AppIconButton>
        ) : undefined
      }
      fullWidth
      onBlur={() => setIsFocused(false)}
      onChange={handleChange}
      onFocus={() => setIsFocused(true)}
      onKeyDown={handleKeyDown}
      placeholder={placeholder}
      size="small"
      startAdornment={
        <Icon
          height={20}
          icon="mdi:magnify"
          style={{
            color: isFocused
              ? theme.palette.primary.main
              : theme.palette.text.secondary,
            transition: "color 0.2s",
          }}
          width={20}
        />
      }
      value={value}
    />
  );
};

export default memo(AppHeaderSearch);
