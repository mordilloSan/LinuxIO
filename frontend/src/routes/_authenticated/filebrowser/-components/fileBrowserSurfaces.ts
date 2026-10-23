/* File-browser surfaces. The theme resolves the chrome and divider tokens
   (--app-chrome-surface-transparent*, --app-divider-subtle) per colour scheme
   in theme/surfaces.ts; these helpers only compose them. */

export const getChromeSurfaceColor = (
  chromeColor: string,
  emphasis: "default" | "hover" = "default",
) =>
  `color-mix(in srgb, ${chromeColor}, transparent var(--app-chrome-surface-transparent${emphasis === "hover" ? "-hover" : ""}))`;

export const getSubtleDividerColor = () => "var(--app-divider-subtle)";

/* Shared by FileCard and FileListRow: a listing entry's resting background,
   keyed off selection and hidden-file dimming. Selected wins over hidden so a
   selected dotfile still reads as selected. */
export const getFileEntryBackground = ({
  hidden,
  selected,
}: {
  hidden?: boolean;
  selected?: boolean;
}) => {
  if (selected) {
    return "color-mix(in srgb, var(--app-palette-primary-main), transparent 60%)";
  }
  if (hidden) {
    return "color-mix(in srgb, var(--app-file-browser-surface), transparent 50%)";
  }
  return "var(--app-file-browser-surface)";
};

/* Brightening the entry's own resting background toward the theme's text
   color — rather than painting a fixed hover color over it — keeps the
   selected and hidden variants distinguishable while hovered. */
export const getFileEntryHoverBackground = (baseBackground: string) =>
  `color-mix(in srgb, ${baseBackground}, var(--app-palette-text-primary) 7%)`;
