import merge from "deepmerge";
import { grey } from "@mui/material/colors";
import { THEMES } from "@/constants";

const customBlue = {
  50: "#e9f0fb",
  100: "#c8daf4",
  200: "#a3c1ed",
  300: "#7ea8e5",
  400: "#6395e0",
  500: "#4782da",
  600: "#407ad6",
  700: "#376fd0",
  800: "#2f65cb",
  900: "#2052c2",
};

const defaultVariant: VariantType = {
  name: THEMES.LIGHT,
  palette: {
    mode: "light",
    primary: {
      main: customBlue[700],
      contrastText: "#FFF",
    },
    secondary: {
      main: customBlue[500],
      contrastText: "#FFF",
    },
    background: {
      default: "#F7F9FC",
      paper: "#FFF",
    },
    text: {
      primary: "rgba(15, 15, 15, 0.95)",
      secondary: "rgba(15, 15, 15, 0.65)",
    },
  },
  header: {
    color: grey[500],
    background: "#F7F9FC",
    search: {
      color: grey[800],
    },
    indicator: {
      background: customBlue[200],
    },
  },
  footer: {
    color: grey[800],
    background: "#F7F9FC",
  },
  sidebar: {
    color: grey[800],
    background: "#F7F9FC",
    header: {
      color: grey[800],
      background: "#F7F9FC",
      brand: {
        color: customBlue[800],
      },
    },
    footer: {
      color: grey[800],
      background: "#F7F9FC",
    },
    badge: {
      color: grey[800],
      background: customBlue[500],
    },
  },
};

const darkVariant: VariantType = merge<VariantType, Partial<VariantType>>(
  defaultVariant,
  {
    name: THEMES.DARK,
    palette: {
      mode: "dark",
      primary: {
        main: customBlue[600],
        contrastText: "#FFF",
      },
      secondary: {
        main: customBlue[400],
        contrastText: "#FFF",
      },
      background: {
        default: "#1B2635",
        paper: "#233044",
      },
      text: {
        primary: "rgba(255, 255, 255, 0.95)",
        secondary: "rgba(255, 255, 255, 0.5)",
      },
    },
    header: {
      color: grey[300],
      background: "#1B2635",
      search: {
        color: grey[200],
      },
      indicator: {
        background: customBlue[600], // or any color you want
      },
    },
    footer: {
      color: grey[300],
      background: "#1B2635",
    },
    sidebar: {
      color: grey[200],
      background: "#1B2635",
      header: {
        color: grey[200],
        background: "#1B2635",
        brand: {
          color: customBlue[500],
        },
      },
      footer: {
        color: grey[200],
        background: "#1E2A38",
      },
      badge: {
        color: "#FFF",
        background: customBlue[500],
      },
    },
  },
);

const variants: Array<VariantType> = [defaultVariant, darkVariant];

export default variants;

export type VariantType = {
  name: string;
  palette: {
    mode: "light" | "dark";
    primary: MainContrastTextType;
    secondary: MainContrastTextType;
    background: {
      default: string;
      paper: string;
    };
    text: {
      primary: string;
      secondary: string;
    };
  };
  header: ColorBgType & {
    search: {
      color: string;
    };
    indicator: {
      background: string;
    };
  };
  footer: ColorBgType;
  sidebar: ColorBgType & {
    header: ColorBgType & {
      brand: {
        color: string;
      };
    };
    footer: ColorBgType;
    badge: ColorBgType;
  };
};

type MainContrastTextType = {
  main: string;
  contrastText: string;
};
type ColorBgType = {
  color: string;
  background: string;
};
