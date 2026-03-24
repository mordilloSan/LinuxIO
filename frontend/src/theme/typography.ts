export type AppTypographyVariant = {
  fontSize: string;
  fontWeight: number;
  lineHeight: number;
  letterSpacing?: string;
};

export type AppTypographyScale = {
  fontFamily: string;
  fontSize: number;
  fontWeightLight: number;
  fontWeightRegular: number;
  fontWeightMedium: number;
  fontWeightBold: number;
  h1: AppTypographyVariant;
  h2: AppTypographyVariant;
  h3: AppTypographyVariant;
  h4: AppTypographyVariant;
  h5: AppTypographyVariant;
  h6: AppTypographyVariant;
  body1: AppTypographyVariant;
  body2: AppTypographyVariant;
  subtitle1: AppTypographyVariant;
  subtitle2: AppTypographyVariant;
  caption: AppTypographyVariant;
  overline: AppTypographyVariant & { textTransform: "uppercase" };
  button: {
    textTransform: "none";
    fontWeight: number;
  };
};

const typography: AppTypographyScale = {
  fontFamily: [
    "Inter",
    "-apple-system",
    "BlinkMacSystemFont",
    '"Segoe UI"',
    "Roboto",
    '"Helvetica Neue"',
    "Arial",
    "sans-serif",
    '"Apple Color Emoji"',
    '"Segoe UI Emoji"',
    '"Segoe UI Symbol"',
  ].join(","),
  fontSize: 13,
  fontWeightLight: 300,
  fontWeightRegular: 400,
  fontWeightMedium: 500,
  fontWeightBold: 600,
  h1: {
    fontSize: "2rem",
    fontWeight: 600,
    lineHeight: 1.25,
  },
  h2: {
    fontSize: "1.75rem",
    fontWeight: 600,
    lineHeight: 1.25,
  },
  h3: {
    fontSize: "1.5rem",
    fontWeight: 600,
    lineHeight: 1.25,
  },
  h4: {
    fontSize: "1.125rem",
    fontWeight: 500,
    lineHeight: 1.25,
  },
  h5: {
    fontSize: "1.0625rem",
    fontWeight: 500,
    lineHeight: 1.25,
  },
  h6: {
    fontSize: "1rem",
    fontWeight: 500,
    lineHeight: 1.25,
  },
  body1: {
    fontSize: "13px",
    fontWeight: 400,
    lineHeight: 1.5,
  },
  body2: {
    fontSize: "0.8125rem",
    fontWeight: 400,
    lineHeight: 1.43,
  },
  subtitle1: {
    fontSize: "0.9286rem",
    fontWeight: 400,
    lineHeight: 1.75,
  },
  subtitle2: {
    fontSize: "0.8125rem",
    fontWeight: 500,
    lineHeight: 1.57,
  },
  caption: {
    fontSize: "0.6964rem",
    fontWeight: 400,
    lineHeight: 1.66,
  },
  overline: {
    fontSize: "0.6964rem",
    fontWeight: 400,
    lineHeight: 2.66,
    textTransform: "uppercase",
  },
  button: {
    textTransform: "none",
    fontWeight: 500,
  },
};

export default typography;
