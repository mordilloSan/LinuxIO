package userconfig

import "fmt"

// ThemeColorSlot is one theme colour override: its API/YAML key and the field
// that holds it.
type ThemeColorSlot struct {
	Key   string
	Value **CSSColor
}

// themeColorSlots is the single list of theme colour keys. Validation,
// cloning and building overrides all iterate it.
func themeColorSlots(colors *ThemeColors) []ThemeColorSlot {
	return []ThemeColorSlot{
		{"backgroundDefault", &colors.BackgroundDefault},
		{"backgroundPaper", &colors.BackgroundPaper},
		{"headerBackground", &colors.HeaderBackground},
		{"footerBackground", &colors.FooterBackground},
		{"sidebarBackground", &colors.SidebarBackground},
		{"cardBackground", &colors.CardBackground},
		{"dialogBorder", &colors.DialogBorder},
		{"dialogGlow", &colors.DialogGlow},
		{"dialogBackdrop", &colors.DialogBackdrop},
		{"codeBackground", &colors.CodeBackground},
		{"codeText", &colors.CodeText},
		{"chartRx", &colors.ChartRx},
		{"chartTx", &colors.ChartTx},
		{"chartNeutral", &colors.ChartNeutral},
		{"fileBrowserSurface", &colors.FileBrowserSurface},
		{"fileBrowserChrome", &colors.FileBrowserChrome},
		{"fileBrowserBreadcrumbBackground", &colors.FileBrowserBreadcrumbBackground},
		{"fileBrowserBreadcrumbText", &colors.FileBrowserBreadcrumbText},
	}
}

// NewThemeColors builds the overrides for one mode from values keyed by theme
// colour key; nil entries are unset. It returns nil when nothing is set and
// an error naming themeColors.<mode>.<key> for the first invalid colour.
func NewThemeColors(values map[string]*string, mode string) (*ThemeColors, error) {
	colors := &ThemeColors{}
	hasAny := false
	for _, slot := range themeColorSlots(colors) {
		raw := values[slot.Key]
		if raw == nil {
			continue
		}
		color, ok := ParseCSSColor(*raw)
		if !ok {
			return nil, fmt.Errorf("invalid themeColors.%s.%s", mode, slot.Key)
		}
		*slot.Value = &color
		hasAny = true
	}
	if !hasAny {
		return nil, nil
	}
	return colors, nil
}
