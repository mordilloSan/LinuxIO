// Themes
export const drawerWidth = 200; //full width
export const collapsedDrawerWidth = 70; // mini sidebar width
export const cardHeight = 220;
export const cardBorderRadius = 16;

// Motion
export const EASING_STANDARD = [0.4, 0, 0.2, 1] as const;
export const EASING_STANDARD_CSS = `cubic-bezier(${EASING_STANDARD.join(", ")})`;
export const EASING_EMPHASIZED = [0.22, 1, 0.36, 1] as const;
export const EASING_EMPHASIZED_CSS = `cubic-bezier(${EASING_EMPHASIZED.join(", ")})`;
export const EASING_DECELERATE = [0, 0, 0.2, 1] as const;
export const EASING_DECELERATE_CSS = `cubic-bezier(${EASING_DECELERATE.join(", ")})`;
export const EASING_LINEAR_PROGRESS_PRIMARY = [0.65, 0.815, 0.74, 0.4] as const;
export const EASING_LINEAR_PROGRESS_PRIMARY_CSS = `cubic-bezier(${EASING_LINEAR_PROGRESS_PRIMARY.join(", ")})`;
export const EASING_LINEAR_PROGRESS_SECONDARY = [0.165, 0.84, 0.44, 1] as const;
export const EASING_LINEAR_PROGRESS_SECONDARY_CSS = `cubic-bezier(${EASING_LINEAR_PROGRESS_SECONDARY.join(", ")})`;
export const TRANSITION_DURATION_FAST_MS = 150;
export const TRANSITION_DURATION_MEDIUM_MS = 200;
export const TRANSITION_DURATION_STANDARD_MS = 250;
export const TRANSITION_DURATION_ENTERING_SCREEN_MS = 225;
export const TRANSITION_DURATION_SLOW_MS = 600;
export const TRANSITION_SLOW_CSS = `${TRANSITION_DURATION_SLOW_MS}ms ${EASING_STANDARD_CSS}`;

export const MOTION_CSS_VARS: Record<string, string> = {
  "--app-easing-standard": EASING_STANDARD_CSS,
  "--app-easing-emphasized": EASING_EMPHASIZED_CSS,
  "--app-easing-decelerate": EASING_DECELERATE_CSS,
  "--app-easing-linear-progress-primary": EASING_LINEAR_PROGRESS_PRIMARY_CSS,
  "--app-easing-linear-progress-secondary":
    EASING_LINEAR_PROGRESS_SECONDARY_CSS,
  "--app-transition-duration-fast": `${TRANSITION_DURATION_FAST_MS}ms`,
  "--app-transition-duration-medium": `${TRANSITION_DURATION_MEDIUM_MS}ms`,
  "--app-transition-duration-standard": `${TRANSITION_DURATION_STANDARD_MS}ms`,
  "--app-transition-duration-entering-screen": `${TRANSITION_DURATION_ENTERING_SCREEN_MS}ms`,
  "--app-transition-duration-slow": `${TRANSITION_DURATION_SLOW_MS}ms`,
};

// Shadows
export const shadowSm = "0px 1px 2px 0px rgba(0, 0, 0, 0.05)";

// Named gap shortcuts (plain numbers for use in style={{ gap: GAP_SM }})
export const GAP_XS = 4;
export const GAP_SM = 6;
export const GAP_MD = 12;
export const GAP_LG = 16;
export const GAP_XL = 24;

// Icon sizes
export const iconSize = {
  sm: 18,
  md: 24,
  lg: 28,
} as const;
