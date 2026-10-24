import { MotionConfig } from "motion/react";
import {
  createContext,
  useContext,
  useInsertionEffect,
  useMemo,
  type ReactNode,
} from "react";

import type { AppTheme } from "@/theme/appTheme";
import { applyCssVariables, getThemeCssVariables } from "@/theme/cssVariables";

interface AppThemeProviderProps {
  children: ReactNode;
  value: AppTheme;
}

const APP_THEME_CONTEXT = createContext<AppTheme | undefined>(undefined);

export function AppThemeProvider({ children, value }: AppThemeProviderProps) {
  const cssVariables = useMemo(() => getThemeCssVariables(value), [value]);

  useInsertionEffect(() => {
    const root = document.documentElement;
    const transitionBlocker = document.createElement("style");
    transitionBlocker.textContent =
      "*,*::before,*::after{transition:none !important}";
    document.head.append(transitionBlocker);

    root.dataset.appTheme = value.name.toLowerCase();
    root.dataset.appColorScheme = value.colorScheme;
    applyCssVariables(root, cssVariables);

    void document.body.offsetHeight;
    let removeFrame = 0;
    const paintFrame = window.requestAnimationFrame(() => {
      removeFrame = window.requestAnimationFrame(() => {
        transitionBlocker.remove();
      });
    });

    return () => {
      window.cancelAnimationFrame(paintFrame);
      window.cancelAnimationFrame(removeFrame);
      transitionBlocker.remove();
    };
  }, [cssVariables, value]);

  return (
    <MotionConfig reducedMotion="user">
      <APP_THEME_CONTEXT.Provider value={value}>
        {children}
      </APP_THEME_CONTEXT.Provider>
    </MotionConfig>
  );
}

export function useAppTheme() {
  const theme = useContext(APP_THEME_CONTEXT);

  if (!theme) {
    throw new Error("useAppTheme must be used within an AppThemeProvider");
  }

  return theme;
}
