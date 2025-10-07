import { Theme, lighten, darken } from "@mui/material/styles";

// === Helpers ===

export const getHoverBackground = (theme: Theme) =>
  theme.palette.mode === "light"
    ? darken(theme.sidebar.background, 0.07)
    : lighten(theme.sidebar.background, 0.07);

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
        width: "8px",
        height: "8px",
      },
      ".custom-scrollbar::-webkit-scrollbar-thumb": {
        backgroundColor: "rgba(100, 100, 100, 0.4)",
        borderRadius: "8px",
        border: "2px solid transparent",
        backgroundClip: "content-box",
        transition: "background-color 0.3s",
      },
      ".custom-scrollbar::-webkit-scrollbar-track": {
        background: "transparent",
        borderRadius: "8px",
      },
      ".custom-scrollbar::-webkit-scrollbar-thumb:hover": {
        backgroundColor: "rgba(100, 100, 100, 0.7)",
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
  MuiTableContainer: {
    styleOverrides: {
      root: ({ theme }: { theme: Theme }) => ({
        backgroundColor:
          theme.palette.mode === "dark"
            ? "rgba(255,255,255,0.08)"
            : "rgba(0,0,0,0.05)",
        borderRadius: "6px",
        boxShadow: "none",
      }),
    },
  },
  MuiTableCell: {
    styleOverrides: {
      root: ({ theme }: { theme: Theme }) => ({
        backgroundColor: "inherit",
        borderBottom: `1px solid ${theme.palette.divider}`,
      }),
    },
  },
  MuiChip: {
    styleOverrides: {
      root: { borderRadius: "6px" },
      filled: {
        textShadow: "0 1px 1px rgba(0, 0, 0, 0.2)",
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
  // Picker styles cleaned
  MuiPickersDay: {
    styleOverrides: {
      day: { fontWeight: "300" },
    },
  },
  MuiPickersYear: {
    styleOverrides: {
      root: { height: "64px" },
    },
  },
  MuiPickersCalendar: {
    styleOverrides: {
      transitionContainer: { marginTop: "6px" },
    },
  },
  MuiPickersCalendarHeader: {
    styleOverrides: {
      iconButton: {
        backgroundColor: "transparent",
        "& > *": { backgroundColor: "transparent" },
      },
      switchHeader: {
        marginTop: "2px",
        marginBottom: "4px",
      },
    },
  },
  MuiPickersClock: {
    styleOverrides: {
      container: { margin: "32px 0 4px" },
    },
  },
  MuiPickersClockNumber: {
    styleOverrides: {
      clockNumber: {
        left: "calc(50% - 16px)",
        width: "32px",
        height: "32px",
      },
    },
  },
  MuiPickerDTHeader: {
    styleOverrides: {
      dateHeader: { "& h4": { fontSize: "2.125rem", fontWeight: 400 } },
      timeHeader: { "& h3": { fontSize: "3rem", fontWeight: 400 } },
    },
  },
  MuiPickersTimePicker: {
    styleOverrides: {
      hourMinuteLabel: { "& h2": { fontSize: "3.75rem", fontWeight: 300 } },
    },
  },
  MuiPickersToolbar: {
    styleOverrides: {
      toolbar: { "& h4": { fontSize: "2.125rem", fontWeight: 400 } },
    },
  },
};

export default components;
