package userconfig

import (
	"fmt"
	"maps"
	"slices"

	"github.com/goccy/go-yaml"
)

// themeColorDefs is the single list of theme colours: the key used in the API
// and in the YAML file, and the colour a new configuration starts with in each
// scheme. Adding a theme colour to the backend is one line here; validation,
// defaults, the order in the file and the API's key type all follow this list.
var themeColorDefs = []struct{ Key, Light, Dark string }{
	{"backgroundDefault", "#F7F9FC", "#1B2635"},
	{"backgroundPaper", "#FFFFFF", "#233044"},
	{"headerBackground", "#F7F9FC", "#1B2635"},
	{"footerBackground", "#F7F9FC", "#1B2635"},
	{"sidebarBackground", "#F7F9FC", "#1B2635"},
	{"cardBackground", "#FFFFFF", "#11192A"},
	{"dialogBorder", "#FFFFFF", "#FFFFFF"},
	{"dialogGlow", "#FFFFFF", "#FFFFFF"},
	{"dialogBackdrop", "#000000", "#000000"},
	{"codeBackground", "#F5F5F5", "#1E1E1E"},
	{"codeText", "#333333", "#D4D4D4"},
	// The surface of code set into a page (the Assistant's code blocks). Unset,
	// the UI uses the page background a shade darker, so it follows
	// backgroundDefault.
	{"codeInsetBackground", "#F0F2F4", "#161F2B"},
	// The box behind a plugin pane's picture in the Assistant, the colour the
	// picture is blended onto. Unset, the UI uses the page background well
	// darker on dark, so it follows backgroundDefault, and the paper colour on
	// light.
	{"panePictureBackground", "#FFFFFF", "#131B25"},
	{"chartRx", "#8884D8", "#8884D8"},
	{"chartTx", "#82CA9D", "#82CA9D"},
	{"chartNeutral", "#808080", "#808080"},
	{"fileBrowserSurface", "#FFFFFF", "#20292F"},
	{"fileBrowserChrome", "#253137", "#253137"},
	{"fileBrowserBreadcrumbBackground", "#D0D4D8", "#283136"},
	{"fileBrowserBreadcrumbText", "#5A5A5A", "#FFFFFF"},
}

// ThemeColorsByMode holds separate color overrides for light and dark mode
type ThemeColorsByMode struct {
	Light ThemeColors `json:"light,omitzero" yaml:"light,omitempty"`
	Dark  ThemeColors `json:"dark,omitzero" yaml:"dark,omitempty"`
}

// ThemeColors holds one scheme's colour overrides by theme colour key. A key
// that is absent is unset.
type ThemeColors map[string]CSSColor

// ThemeColorKeys lists the theme colour keys in their file order.
func ThemeColorKeys() []string {
	keys := make([]string, len(themeColorDefs))
	for i, def := range themeColorDefs {
		keys[i] = def.Key
	}
	return keys
}

// IsThemeColorKey reports whether key names a theme colour.
func IsThemeColorKey(key string) bool {
	return slices.ContainsFunc(themeColorDefs, func(def struct{ Key, Light, Dark string }) bool {
		return def.Key == key
	})
}

func defaultThemeColors() *ThemeColorsByMode {
	light := make(ThemeColors, len(themeColorDefs))
	dark := make(ThemeColors, len(themeColorDefs))
	for _, def := range themeColorDefs {
		light[def.Key] = CSSColor(def.Light)
		dark[def.Key] = CSSColor(def.Dark)
	}
	return &ThemeColorsByMode{Light: light, Dark: dark}
}

// UnmarshalYAML reads the document over the colours already there, so a sparse
// document keeps the defaults it does not mention: a key replaces that colour,
// a null key unsets it and a null scheme unsets the scheme. An unknown scheme
// or key is refused, as strict decoding refuses an unknown field.
func (m *ThemeColorsByMode) UnmarshalYAML(data []byte) error {
	var document map[string]map[string]*CSSColor
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	for mode, colors := range document {
		var scheme *ThemeColors
		switch mode {
		case "light":
			scheme = &m.Light
		case "dark":
			scheme = &m.Dark
		default:
			return fmt.Errorf("unknown field %q", mode)
		}
		if colors == nil {
			*scheme = nil
			continue
		}
		merged := maps.Clone(*scheme)
		if merged == nil {
			merged = make(ThemeColors, len(colors))
		}
		for key, color := range colors {
			if !IsThemeColorKey(key) {
				return fmt.Errorf("unknown field %q", key)
			}
			if color == nil {
				delete(merged, key)
				continue
			}
			merged[key] = *color
		}
		*scheme = merged
	}
	return nil
}

// MarshalYAML writes the colours in the order of themeColorDefs, so the file
// does not reorder when a colour changes.
func (c ThemeColors) MarshalYAML() (any, error) {
	document := make(yaml.MapSlice, 0, len(c))
	for _, def := range themeColorDefs {
		if color, ok := c[def.Key]; ok {
			document = append(document, yaml.MapItem{Key: def.Key, Value: string(color)})
		}
	}
	return document, nil
}

// NewThemeColors builds the overrides for one mode from values keyed by theme
// colour key; nil entries are unset and keys that are not theme colours are
// ignored. It returns nil when nothing is set and an error naming
// themeColors.<mode>.<key> for the first invalid colour.
func NewThemeColors(values map[string]*string, mode string) (ThemeColors, error) {
	var colors ThemeColors
	for _, def := range themeColorDefs {
		raw := values[def.Key]
		if raw == nil {
			continue
		}
		color, ok := ParseCSSColor(*raw)
		if !ok {
			return nil, fmt.Errorf("invalid themeColors.%s.%s", mode, def.Key)
		}
		if colors == nil {
			colors = make(ThemeColors)
		}
		colors[def.Key] = color
	}
	return colors, nil
}
