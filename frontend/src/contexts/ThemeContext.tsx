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
  sidebarColapsed: SIDEBAR_COLAPSED_STATE,
  setSidebarColapsed: () => {},
  toggleTheme: () => {},
};

const ThemeContext = createContext<ThemeContextType>(initialState);

const ThemeProvider: React.FC<ThemeProviderProps> = ({ children }) => {
  const [theme, _setTheme] = useState(initialState.theme);
  const [primaryColor, _setPrimaryColor] = useState(DEFAULT_PRIMARY_COLOR);
  const [sidebarColapsed, _setSidebarColapsed] = useState(
    SIDEBAR_COLAPSED_STATE,
  );
  const [isLoaded, setIsLoaded] = useState(false);

  useEffect(() => {
    const fetchTheme = async () => {
      try {
        const response = await axios.get("/theme/get");
        const fetchedTheme =
          response.data.theme === "LIGHT" ? THEMES.LIGHT : THEMES.DARK;
        const fetchedColor = response.data.primaryColor;
        const fetchedColapsed = response.data.sidebarColapsed;
        _setTheme(fetchedTheme);
        _setPrimaryColor(fetchedColor || DEFAULT_PRIMARY_COLOR);
        _setSidebarColapsed(fetchedColapsed ?? SIDEBAR_COLAPSED_STATE);
        setIsLoaded(true);
      } catch (error) {
        console.error("Error fetching theme from backend:", error);
      }
    };

    fetchTheme();
  }, []);

  const debouncedSaveThemeSettings = useMemo(() => {
    return debounce(
      (themeToSave: string, colorToSave: string, colapsed: boolean) => {
        axios
          .post("/theme/set", {
            theme: themeToSave,
            primaryColor: colorToSave,
            sidebarColapsed: colapsed,
          })
          .catch((error) => {
            console.error("Error saving theme settings:", error);
          });
      },
      500,
    ); // Save only after 500ms of inactivity
  }, []);

  const setTheme = useCallback(
    (newTheme: string) => {
      _setTheme(newTheme);
      debouncedSaveThemeSettings(newTheme, primaryColor, sidebarColapsed);
    },
    [primaryColor, sidebarColapsed, debouncedSaveThemeSettings],
  );

  const setPrimaryColor = useCallback(
    (color: string) => {
      _setPrimaryColor(color);
      debouncedSaveThemeSettings(theme, color, sidebarColapsed);
    },
    [theme, sidebarColapsed, debouncedSaveThemeSettings],
  );

  const setSidebarColapsed = useCallback(
    (valueOrUpdater: boolean | ((prev: boolean) => boolean)) => {
      _setSidebarColapsed((prev) => {
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
    const newTheme = theme === THEMES.DARK ? THEMES.LIGHT : THEMES.DARK;
    setTheme(newTheme);
  }, [theme, setTheme]);

  const contextValue = useMemo(
    () => ({
      theme,
      setTheme,
      primaryColor,
      setPrimaryColor,
      sidebarColapsed,
      setSidebarColapsed,
      toggleTheme,
      isLoaded,
    }),
    [
      theme,
      primaryColor,
      sidebarColapsed,
      setTheme,
      setPrimaryColor,
      setSidebarColapsed,
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

export { ThemeProvider, ThemeContext };
