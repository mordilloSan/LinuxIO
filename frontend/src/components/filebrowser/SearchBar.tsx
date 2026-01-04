import ClearIcon from "@mui/icons-material/Clear";
import SearchIcon from "@mui/icons-material/Search";
import { IconButton, InputAdornment, TextField, useTheme } from "@mui/material";
import React, { useState, useCallback } from "react";

interface SearchBarProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
}

const SearchBar: React.FC<SearchBarProps> = ({
  value,
  onChange,
  placeholder = "Search files...",
  disabled = false,
}) => {
  const theme = useTheme();
  const [isFocused, setIsFocused] = useState(false);

  const handleClear = useCallback(() => {
    onChange("");
  }, [onChange]);

  const handleChange = useCallback(
    (event: React.ChangeEvent<HTMLInputElement>) => {
      onChange(event.target.value);
    },
    [onChange],
  );

  return (
    <TextField
      value={value}
      onChange={handleChange}
      onFocus={() => setIsFocused(true)}
      onBlur={() => setIsFocused(false)}
      placeholder={placeholder}
      disabled={disabled}
      size="small"
      fullWidth
      slotProps={{
        input: {
          startAdornment: (
            <InputAdornment position="start">
              <SearchIcon
                fontSize="small"
                sx={{
                  color: isFocused
                    ? theme.palette.primary.main
                    : theme.palette.text.secondary,
                  transition: "color 0.2s",
                }}
              />
            </InputAdornment>
          ),
          endAdornment: value && (
            <InputAdornment position="end">
              <IconButton
                onClick={handleClear}
                size="small"
                edge="end"
                aria-label="clear search"
                sx={{
                  padding: 0.5,
                }}
              >
                <ClearIcon fontSize="small" />
              </IconButton>
            </InputAdornment>
          ),
        },
      }}
      sx={{
        maxWidth: 400,
        minWidth: 250,
        "& .MuiOutlinedInput-root": {
          borderRadius: "24px",
          backgroundColor:
            "color-mix(in srgb, var(--mui-palette-grey-500), transparent 90%)",
          transition: "all 0.2s",
          "& fieldset": {
            border: "none",
          },
          "&:hover": {
            backgroundColor:
              "color-mix(in srgb, var(--mui-palette-grey-500), transparent 80%)",
          },
        },
      }}
    />
  );
};

export default SearchBar;
