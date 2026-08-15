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
/* The dock's magnification spring (stiffness 170, damping 14, mass 0.1 — see
   useDockMagnification.ts), sampled as an easing curve so CSS hover states move
   with the same physics as the dock tiles. The spring is overdamped (ζ ≈ 1.70),
   so it never overshoots: what reads as "alive" is the fast attack and the long
   decelerating tail, not bounce. Points are the step response at 16 even
   intervals across TRANSITION_DURATION_LIFT_MS; regenerate both together if the
   dock's spring constants change. */
export const EASING_SPRING_CSS =
  "linear(0, 0.1543, 0.347, 0.5003, 0.618, 0.708, 0.7768, 0.8294, 0.8696, 0.9003, 0.9238, 0.9417, 0.9555, 0.966, 0.974, 0.9801, 1)";
export const TRANSITION_DURATION_FAST_MS = 150;
export const TRANSITION_DURATION_MEDIUM_MS = 200;
export const TRANSITION_DURATION_STANDARD_MS = 250;
export const TRANSITION_DURATION_SLOW_MS = 600;
/* Time for EASING_SPRING_CSS to reach ~98.5% of the way — past that the spring
   is still settling but the movement is no longer visible. */
export const TRANSITION_DURATION_LIFT_MS = 320;
export const TRANSITION_SLOW_CSS = `${TRANSITION_DURATION_SLOW_MS}ms ${EASING_STANDARD_CSS}`;

export const MOTION_CSS_VARS: Record<string, string> = {
  "--app-easing-standard": EASING_STANDARD_CSS,
  "--app-easing-emphasized": EASING_EMPHASIZED_CSS,
  "--app-easing-decelerate": EASING_DECELERATE_CSS,
  "--app-easing-linear-progress-primary": EASING_LINEAR_PROGRESS_PRIMARY_CSS,
  "--app-easing-linear-progress-secondary":
    EASING_LINEAR_PROGRESS_SECONDARY_CSS,
  "--app-easing-spring": EASING_SPRING_CSS,
  "--app-transition-duration-fast": `${TRANSITION_DURATION_FAST_MS}ms`,
  "--app-transition-duration-lift": `${TRANSITION_DURATION_LIFT_MS}ms`,
  "--app-transition-duration-medium": `${TRANSITION_DURATION_MEDIUM_MS}ms`,
  "--app-transition-duration-standard": `${TRANSITION_DURATION_STANDARD_MS}ms`,
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

// Canonical dashboard card spacing is expressed in AppGrid units. Custom CSS
// and virtualized grids use the derived pixel gap.
export const DASHBOARD_CARD_SPACING = 4;
export const DASHBOARD_CARD_GAP = DASHBOARD_CARD_SPACING * GAP_XS;

/* Vertical room a hover-lifted card needs above its resting top edge. The lift
   itself is a 4px translate (--hover-lift-y on .fc-hover-lift, which every card
   grid uses); the remaining 2px keeps it clear of whatever sits above. Without
   this the top row is cut off along its top edge — either occluded by an opaque
   sticky header or clipped by a scroll container's overflow.
   Mirrors --tab-strip-headroom in components/tabbar/tab-container.css. */
export const HOVER_LIFT_HEADROOM = 6;

/* Sideways room a lifted card's shadow needs when a card grid scrolls inside
   its own scrollport. `overflow: auto` clips, and getFrostedCardLiftShadow is
   `0 8px 24px` — a 24px blur reaches about half that far to each side before it
   is invisible, so an edge card would otherwise show a hard vertical cut along
   the scrollport wall on hover. There is no equivalent above, where the lift
   headroom already reserves what the tab strip hands back. */
export const CARD_LIFT_SHADOW_GUTTER = 12;

// Icon sizes
export const iconSize = {
  sm: 18,
  md: 24,
  lg: 28,
} as const;
