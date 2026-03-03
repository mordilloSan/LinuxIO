import "@mui/material/styles";
import "@mui/material/TextField";

declare module "@mui/material/TextField" {
  interface TextFieldPropsVariantOverrides {
    search: true;
  }
}

declare module "@mui/material/styles" {
  interface Theme {
    card: {
      background: string;
    };
    dialog: {
      border: string;
      glow: string;
      backdrop: string;
    };
    codeBlock: {
      background: string;
      color: string;
    };
    chart: {
      rx: string;
      tx: string;
      neutral: string;
    };
    fileBrowser: {
      surface: string;
      chrome: string;
      breadcrumbBackground: string;
      breadcrumbText: string;
    };
    footer: {
      color: string;
      background: string;
    };
    header: {
      color: string;
      background: string;
      search: {
        color: string;
      };
      indicator: {
        background: string;
      };
    };
    sidebar: {
      color: string;
      background: string;
      header: {
        color: string;
        background: string;
        brand: {
          color: string;
        };
      };
      badge: {
        color: string;
        background: string;
      };
    };
  }

  interface ThemeOptions {
    card?: {
      background?: string;
    };
    dialog?: {
      border?: string;
      glow?: string;
      backdrop?: string;
    };
    codeBlock?: {
      background?: string;
      color?: string;
    };
    chart?: {
      rx?: string;
      tx?: string;
      neutral?: string;
    };
    fileBrowser?: {
      surface?: string;
      chrome?: string;
      breadcrumbBackground?: string;
      breadcrumbText?: string;
    };
    footer?: {
      color?: string;
      background?: string;
    };
    header?: {
      color?: string;
      background?: string;
      search?: {
        color?: string;
      };
      indicator?: {
        background?: string;
      };
    };
    sidebar?: {
      color?: string;
      background?: string;
      header?: {
        color?: string;
        background?: string;
        brand?: {
          color?: string;
        };
      };
      badge?: {
        color?: string;
        background?: string;
      };
    };
  }
}
