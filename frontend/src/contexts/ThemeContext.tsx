// src/contexts/ThemeContext.tsx
import React, {
  createContext,
  useEffect,
  useState,
  useCallback,
  useMemo,
} from "react";

import {
  DEFAULT_PRIMARY_COLOR,
  SIDEBAR_COLAPSED_STATE,
  THEMES,
} from "@/constants";
import { ThemeContextType, ThemeProviderProps } from "@/types/theme";
import axios from "@/utils/axios";
import { debounce } from "@/utils/debounce";

const initialState: ThemeContextType = {
  theme: THEMES.DARK,
  setTheme: () => {},
  primaryColor: DEFAULT_PRIMARY_COLOR,
  setPrimaryColor: () => {},
  SidebarCollapsed: SIDEBAR_COLAPSED_STATE,
  setSidebarCollapsed: () => {},
  toggleTheme: () => {},
  isLoaded: false,
};

export const ThemeContext = createContext<ThemeContextType>(initialState);

const LS_KEY = "linuxio:lastTheme";

export const ThemeProvider: React.FC<ThemeProviderProps> = ({ children }) => {
  const [theme, _setTheme] = useState(initialState.theme);
  const [primaryColor, _setPrimaryColor] = useState(DEFAULT_PRIMARY_COLOR);
  const [SidebarCollapsed, _setSidebarCollapsed] = useState(
    SIDEBAR_COLAPSED_STATE,
  );
  const [isLoaded, setIsLoaded] = useState(false);
  const [canPersist, setCanPersist] = useState(false); // only POST after a successful GET

  // 2) Try to fetch the user’s theme (works if logged in; returns 401 if not)
  useEffect(() => {
    let cancelled = false;

    (async () => {
      try {
        const res = await axios.get("/theme/get", { withCredentials: true });
        if (cancelled) return;

        if (res.status === 200 && res.data) {
          const data = res.data;
          const fetchedTheme =
            String(data.theme).toUpperCase() === "LIGHT"
              ? THEMES.LIGHT
              : THEMES.DARK;

          _setTheme(fetchedTheme);
          _setPrimaryColor(data.primaryColor || DEFAULT_PRIMARY_COLOR);
          _setSidebarCollapsed(
            typeof data.sidebarCollapsed === "boolean"
              ? data.sidebarCollapsed
              : SIDEBAR_COLAPSED_STATE,
          );

          // cache last-good
          localStorage.setItem(
            LS_KEY,
            JSON.stringify({
              theme: fetchedTheme,
              primaryColor: data.primaryColor || DEFAULT_PRIMARY_COLOR,
              SidebarCollapsed:
                typeof data.sidebarCollapsed === "boolean"
                  ? data.sidebarCollapsed
                  : SIDEBAR_COLAPSED_STATE,
            }),
          );

          setCanPersist(true); // we’re authenticated; allow POSTs
        } else {
          setCanPersist(false);
        }
      } catch {
        // 401 / network error → unauthenticated or backend down
        setCanPersist(false);
      } finally {
        if (!cancelled) setIsLoaded(true);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  // Debounced saver (no-op until canPersist becomes true)
  const debouncedSaveThemeSettings = useMemo(
    () =>
      debounce(
        (themeToSave: string, colorToSave: string, collapsed: boolean) => {
          if (!canPersist) return;
          axios
            .post(
              "/theme/set",
              {
                theme: themeToSave,
                primaryColor: colorToSave,
                sidebarCollapsed: collapsed, // new camelCase field
              },
              { withCredentials: true },
            )
            .catch(() => {
              /* ignore to keep UX clean */
            });
        },
        400,
      ),
    [canPersist],
  );

  const setTheme = useCallback(
    (newTheme: string) => {
      _setTheme(newTheme);
      debouncedSaveThemeSettings(newTheme, primaryColor, SidebarCollapsed);
    },
    [primaryColor, SidebarCollapsed, debouncedSaveThemeSettings],
  );

  const setPrimaryColor = useCallback(
    (color: string) => {
      _setPrimaryColor(color);
      debouncedSaveThemeSettings(theme, color, SidebarCollapsed);
    },
    [theme, SidebarCollapsed, debouncedSaveThemeSettings],
  );

  const setSidebarCollapsed = useCallback(
    (valueOrUpdater: boolean | ((prev: boolean) => boolean)) => {
      _setSidebarCollapsed((prev) => {
        const newValue =
          typeof valueOrUpdater === "function"
            ? valueOrUpdater(prev)
            : valueOrUpdater;
        debouncedSaveThemeSettings(theme, primaryColor, newValue);
        return newValue;
      });
    },
    [theme, primaryColor, debouncedSaveThemeSettings],
  );

  const toggleTheme = useCallback(() => {
    const next = theme === THEMES.DARK ? THEMES.LIGHT : THEMES.DARK;
    setTheme(next);
  }, [theme, setTheme]);

  const contextValue = useMemo(
    () => ({
      theme,
      setTheme,
      primaryColor,
      setPrimaryColor,
      SidebarCollapsed,
      setSidebarCollapsed,
      toggleTheme,
      isLoaded,
    }),
    [
      theme,
      primaryColor,
      SidebarCollapsed,
      setTheme,
      setPrimaryColor,
      setSidebarCollapsed,
      toggleTheme,
      isLoaded,
    ],
  );

  return (
    <ThemeContext.Provider value={contextValue}>
      {children}
    </ThemeContext.Provider>
  );
};
