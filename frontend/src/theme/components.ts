import { alpha, Theme } from "@mui/material/styles";

// === Helpers ===

export const getHoverBackground = (theme: Theme) =>
  theme.palette.mode === "light"
    ? theme.darken(theme.sidebar.background, 0.07)
    : theme.lighten(theme.sidebar.background, 0.07);

const hoverStyles = (theme: Theme) => ({
  background: getHoverBackground(theme),
  boxShadow: "0 2px 8px rgba(0,0,0,0.05)",
});

const transitionStyles = (theme: Theme) => ({
  transition: theme.transitions.create(
    ["background-color", "color", "transform"],
    {
      duration: theme.transitions.duration.shortest,
    },
  ),
});

const getChipColor = (theme: Theme, color?: string) => {
  switch (color) {
    case "primary":
      return theme.palette.primary.main;
    case "secondary":
      return theme.palette.secondary.main;
    case "success":
      return theme.palette.success.main;
    case "error":
      return theme.palette.error.main;
    case "warning":
      return theme.palette.warning.main;
    case "info":
      return theme.palette.info.main;
    case "default":
    default:
      return theme.palette.text.secondary;
  }
};

// === Components ===

const components = {
  MuiCssBaseline: {
    styleOverrides: () => ({
      html: { height: "100%" },
      body: { height: "100%", margin: 0 },
      "#root": { height: "100%" },
      /* Xterm.js bugfix: hide helper textarea */
      ".xterm-helper-textarea, .xterm-textarea": {
        opacity: 0,
        position: "absolute",
        left: "-9999px",
        width: 0,
        height: 0,
        zIndex: -1,
        pointerEvents: "none",
        background: "transparent",
      },
      /* Make scrollbars beautiful */
      ".custom-scrollbar::-webkit-scrollbar": {
        width: "8px !important",
        height: "8px !important",
      },
      ".custom-scrollbar::-webkit-scrollbar-thumb": {
        backgroundColor: "rgba(100, 100, 100, 0.2) !important",
        borderRadius: "8px !important",
        border: "2px solid transparent !important",
        backgroundClip: "content-box !important",
        transition: "background-color 0.3s !important",
      },
      ".custom-scrollbar::-webkit-scrollbar-track": {
        background: "transparent !important",
        borderRadius: "8px !important",
      },
      ".custom-scrollbar::-webkit-scrollbar-thumb:hover": {
        backgroundColor: "rgba(100, 100, 100, 0.45) !important",
      },
      /* Service card detail rows — remove border from last row */
      ".svc-detail-row:last-child": {
        borderBottom: "none",
      },

      /* xterm.js 6.0 custom scrollbar */
      ".xterm .scrollbar": {
        width: "4px !important",
        opacity: "1 !important",
      },
      ".xterm .scrollbar .slider": {
        backgroundColor: "rgba(100, 100, 100, 0.2) !important",
        borderRadius: "8px !important",
        width: "8px !important",
      },
      ".xterm .scrollbar:hover .slider": {
        backgroundColor: "rgba(100, 100, 100, 0.45) !important",
      },
      ".xterm .scrollbar.fade": {
        opacity: "0 !important",
        transition: "opacity 0.3s !important",
      },
      ".xterm:hover .scrollbar.fade": {
        opacity: "1 !important",
      },
    }),
  },
  MuiButtonBase: {
    defaultProps: {
      disableRipple: true,
    },
  },
  MuiButton: {
    styleOverrides: {
      contained: {
        textShadow: "0 1px 1px rgba(0, 0, 0, 0.3)",
        boxShadow: "rgba(0, 0, 0, 0.05) 0 2px 4px 0",
      },
    },
  },
  MuiListItemButton: {
    styleOverrides: {
      root: (params: { theme: Theme }) => {
        const { theme } = params;
        return {
          ...transitionStyles(theme),
          "&:hover": hoverStyles(theme),
        };
      },
    },
  },
  MuiIconButton: {
    styleOverrides: {
      root: (params: { theme: Theme }) => {
        const { theme } = params;
        return {
          ...transitionStyles(theme),
          "&:hover": hoverStyles(theme),
        };
      },
    },
  },
  MuiLink: {
    defaultProps: {
      underline: "hover",
    },
  },
  MuiCardHeader: {
    defaultProps: {
      titleTypographyProps: { variant: "h6" },
    },
    styleOverrides: {
      action: {
        marginTop: "-4px",
        marginRight: "-4px",
      },
    },
  },
  MuiCard: {
    styleOverrides: {
      root: {
        borderRadius: "6px",
        boxShadow:
          "rgba(50, 50, 93, 0.025) 0px 2px 5px -1px, rgba(0, 0, 0, 0.05) 0px 1px 3px -1px",
        backgroundImage: "none",
      },
    },
  },
  MuiPaper: {
    styleOverrides: {
      root: { backgroundImage: "none" },
    },
  },
  MuiChip: {
    styleOverrides: {
      root: (params: {
        theme: Theme;
        ownerState: { color?: string; variant?: string; disabled?: boolean };
      }) => {
        const { theme, ownerState } = params;
        const isFilled = (ownerState.variant ?? "filled") === "filled";
        const chipColor = getChipColor(theme, ownerState.color);

        return {
          borderRadius: "999px",
          fontWeight: 500,
          boxSizing: "border-box",
          ...(isFilled &&
            !ownerState.disabled && {
              color: chipColor,
              backgroundColor: alpha(
                chipColor,
                theme.palette.mode === "dark" ? 0.2 : 0.14,
              ),
              border: `1px solid ${alpha(
                chipColor,
                theme.palette.mode === "dark" ? 0.42 : 0.28,
              )}`,
            }),
        };
      },
      filled: {
        textShadow: "none",
      },
    },
  },
  MuiMenu: {
    styleOverrides: {
      paper: (params: { theme: Theme }) => {
        const { theme } = params;
        return {
          borderColor: theme.palette.divider,
        };
      },
    },
  },
};

export default components;
