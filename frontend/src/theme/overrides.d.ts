import "@mui/material/styles";

declare module "@mui/material/styles" {
  interface Theme {
    card: {
      background: string;
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
