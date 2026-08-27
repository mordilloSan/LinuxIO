/* The app's only viewport scale. JS reads it through theme.breakpoints
   (up("sm") → (min-width: 770px), down("sm") → (max-width: 769.95px); the
   0.05 offset keeps an up/down pair from both matching at the boundary).
   Media queries cannot read custom properties, so stylesheets hand-mirror
   these values in those two forms — grep the pixel value before changing
   one here.

   CSS additionally uses one sub-sm boundary: "compact", at
   (min-width: 600px) / (max-width: 599.95px), for phone-density tweaks
   (dialog margins, table cell padding) that are tighter than sm. */
const breakpoints = {
  values: {
    xs: 0,
    sm: 770,
    md: 960,
    lg: 1380,
    xl: 1740,
  },
};

export type BreakpointKey = keyof typeof breakpoints.values;

/* Media-query strings for useAppMediaQuery. Pure functions of the scale, so a
   component can size itself without subscribing to the theme. */
export const up = (key: BreakpointKey) =>
  `@media (min-width:${breakpoints.values[key]}px)`;
export const down = (key: BreakpointKey) =>
  `@media (max-width:${Math.max(breakpoints.values[key] - 0.05, 0)}px)`;
export const between = (start: BreakpointKey, end: BreakpointKey) =>
  `@media (min-width:${breakpoints.values[start]}px) and (max-width:${Math.max(breakpoints.values[end] - 0.05, 0)}px)`;

export default breakpoints;
