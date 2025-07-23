import { Theme } from "@mui/material";

export type ThemeProps = {
  theme: Theme & { palette: any };
};

export type ThemeContextType = {
  theme: string;
  setTheme: (theme: string) => void;
  primaryColor: string;
  setPrimaryColor: (color: string) => void;
  sidebarColapsed: boolean;
  setSidebarColapsed: (value: boolean | ((prev: boolean) => boolean)) => void;
  toggleTheme: () => void;
  isLoaded?: boolean;
};

export type ThemeProviderProps = {
  children: React.ReactNode;
};
