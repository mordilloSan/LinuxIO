import { GREY_TOKENS as grey } from "@/theme/colors";

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
  name: "LIGHT",
  card: {
    background: "#FFFFFF",
  },
  dialog: {
    border: "#FFFFFF",
    glow: "#FFFFFF",
    backdrop: "#000000",
  },
  codeBlock: {
    background: "#F5F5F5",
    color: "#333333",
  },
  chart: {
    rx: "#8884D8",
    tx: "#82CA9D",
    neutral: "#808080",
  },
  fileBrowser: {
    surface: "#FFFFFF",
    chrome: "#253137",
    breadcrumbBackground: "#D0D4D8",
    breadcrumbText: "#5A5A5A",
  },
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
    badge: {
      color: grey[800],
      background: customBlue[500],
    },
  },
};

const darkVariant: VariantType = {
  ...defaultVariant,
  name: "DARK",
  card: {
    background: "#11192A",
  },
  dialog: {
    border: "#FFFFFF",
    glow: "#FFFFFF",
    backdrop: "#000000",
  },
  codeBlock: {
    background: "#1E1E1E",
    color: "#D4D4D4",
  },
  chart: {
    rx: "#8884D8",
    tx: "#82CA9D",
    neutral: "#808080",
  },
  fileBrowser: {
    surface: "#20292F",
    chrome: "#253137",
    breadcrumbBackground: "#283136",
    breadcrumbText: "#FFFFFF",
  },
  palette: {
    ...defaultVariant.palette,
    mode: "dark",
    primary: { main: customBlue[600], contrastText: "#FFF" },
    secondary: { main: customBlue[400], contrastText: "#FFF" },
    background: { default: "#1B2635", paper: "#233044" },
    text: {
      primary: "rgba(255, 255, 255, 0.95)",
      secondary: "rgba(255, 255, 255, 0.5)",
    },
  },
  header: {
    ...defaultVariant.header,
    color: grey[300],
    background: "#1B2635",
    search: { color: grey[200] },
    indicator: { background: customBlue[600] },
  },
  footer: {
    ...defaultVariant.footer,
    color: grey[300],
    background: "#1B2635",
  },
  sidebar: {
    ...defaultVariant.sidebar,
    color: grey[200],
    background: "#1B2635",
    header: {
      ...defaultVariant.sidebar.header,
      color: grey[200],
      background: "#1B2635",
      brand: { color: customBlue[500] },
    },
    badge: {
      ...defaultVariant.sidebar.badge,
      color: "#FFF",
      background: customBlue[500],
    },
  },
};

const variants: VariantType[] = [defaultVariant, darkVariant];

export default variants;

export interface VariantType {
  name: string;
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
    badge: ColorBgType;
  };
}

interface MainContrastTextType {
  main: string;
  contrastText: string;
}
interface ColorBgType {
  color: string;
  background: string;
}
