import "@mui/material/styles";

declare module "@mui/material/styles" {
  interface Theme {
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
      footer: {
        color: string;
        background: string;
      };
      badge: {
        color: string;
        background: string;
      };
    };
  }

  interface ThemeOptions {
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
      footer?: {
        color?: string;
        background?: string;
      };
      badge?: {
        color?: string;
        background?: string;
      };
    };
  }
}
