package userconfig

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestValidChunkSizeMBFitsOneRelayFrame(t *testing.T) {
	require.Equal(t, relay.MaxRelayPayloadSize, MaxChunkSizeMB<<20)
	require.True(t, ValidChunkSizeMB(0))
	require.True(t, ValidChunkSizeMB(1))
	require.True(t, ValidChunkSizeMB(MaxChunkSizeMB))
	require.False(t, ValidChunkSizeMB(MaxChunkSizeMB+1))
	require.False(t, ValidChunkSizeMB(-1))
}

func TestParseDockerFolders(t *testing.T) {
	folders, err := ParseDockerFolders([]string{" /srv/docker/ ", "/mnt/stacks"})
	require.NoError(t, err)
	require.Equal(t, []AbsolutePath{"/srv/docker", "/mnt/stacks"}, folders)

	for name, values := range map[string][]string{
		"empty list": {},
		"empty path": {"  "},
		"relative":   {"docker"},
		"root":       {"/"},
		"duplicate":  {"/srv/docker", "/srv/docker/"},
	} {
		_, err := ParseDockerFolders(values)
		require.Error(t, err, name)
	}
}

func TestNewThemeColorsValidatesAndSkipsUnset(t *testing.T) {
	colors, err := NewThemeColors(map[string]*string{"chartRx": new(" #123456 "), "codeText": nil, "nope": new("#000000")}, "dark")
	require.NoError(t, err)
	require.Equal(t, ThemeColors{"chartRx": "#123456"}, colors)

	colors, err = NewThemeColors(map[string]*string{}, "dark")
	require.NoError(t, err)
	require.Nil(t, colors)

	_, err = NewThemeColors(map[string]*string{"codeText": new("nope")}, "light")
	require.EqualError(t, err, "invalid themeColors.light.codeText")
}

func TestNewThemeColorsTakesEveryThemeColor(t *testing.T) {
	for _, key := range ThemeColorKeys() {
		colors, err := NewThemeColors(map[string]*string{key: new("#161F2B")}, "dark")
		require.NoError(t, err, key)
		require.Equal(t, ThemeColors{key: "#161F2B"}, colors, key)
	}
}

func TestThemeColorDefsAreUniqueWithValidDefaults(t *testing.T) {
	keys := ThemeColorKeys()
	seen := map[string]bool{}
	for _, key := range keys {
		require.False(t, seen[key], key)
		seen[key] = true
	}
	defaults := defaultThemeColors()
	for _, colors := range []ThemeColors{defaults.Light, defaults.Dark} {
		for key, color := range colors {
			require.True(t, IsThemeColorKey(key), key)
			require.True(t, isValidCSSColor(string(color)), key)
		}
		// The UI derives these two from the page background, so the backend
		// gives them no default that a load would merge back in.
		require.NotContains(t, colors, "codeInsetBackground")
		require.NotContains(t, colors, "panePictureBackground")
		require.Len(t, colors, len(keys)-2)
	}
	ui := DefaultUIPreferences()
	require.Empty(t, validateThemeColors(&ui))
}

func TestThemeColorsLoadOverTheDefaults(t *testing.T) {
	load := func(document string) (*ThemeColorsByMode, error) {
		ui, err := parseUIConfig([]byte(document))
		if err != nil {
			return nil, err
		}
		return ui.ThemeColors, nil
	}
	defaults := defaultThemeColors()

	// A sparse document changes the colours it names and keeps the rest.
	colors, err := load("themeColors:\n  dark:\n    codeText: \"  #111111  \"\n    chartRx: null\n")
	require.NoError(t, err)
	want := maps.Clone(defaults.Dark)
	want["codeText"] = "#111111"
	delete(want, "chartRx")
	require.Equal(t, want, colors.Dark)
	require.Equal(t, defaults.Light, colors.Light)

	colors, err = load("themeColors:\n  light: null\n  dark: {}\n")
	require.NoError(t, err)
	require.Nil(t, colors.Light)
	require.Equal(t, defaults.Dark, colors.Dark)

	colors, err = load("themeColors: null\n")
	require.NoError(t, err)
	require.Nil(t, colors)

	// Strict decoding: a key that is not a theme colour is refused.
	_, err = load("themeColors:\n  dark:\n    nope: \"#111111\"\n")
	require.ErrorContains(t, err, `unknown field "nope"`)
	_, err = load("themeColors:\n  dark:\n    codeText: zzz\n")
	require.ErrorContains(t, err, `invalid CSS color "zzz"`)
	_, err = load("themeColors:\n  dark: 3\n")
	require.Error(t, err)
}

func TestThemeColorsAreWrittenInTableOrder(t *testing.T) {
	out, err := yaml.Marshal(ThemeColorsByMode{Dark: ThemeColors{"chartRx": "red", "backgroundPaper": "#233044", "codeText": ""}})
	require.NoError(t, err)
	require.Equal(t, "dark:\n  backgroundPaper: \"#233044\"\n  codeText: \"\"\n  chartRx: red\n", string(out))
}

func TestValidateThemeColorsNamesTheKey(t *testing.T) {
	ui := DefaultUIPreferences()
	ui.ThemeColors = &ThemeColorsByMode{Light: ThemeColors{"codeText": "zzz", "nope": "#000000", "chartRx": ""}}
	require.Equal(t, []string{
		"themeColors.light.codeText must be a valid CSS color",
		"themeColors.light.nope is not a theme color",
	}, validateThemeColors(&ui))
}

func TestValidateJobSettingsReportsNegatives(t *testing.T) {
	require.Empty(t, ValidateJobSettings(DefaultJobSettings()))
	require.Equal(t, []string{"jobs.progressMinBytesMB must be >= 0"}, ValidateJobSettings(PersistedJobSettings{ProgressMinBytesMB: -1}))
}

func TestValidateCollapsedSectionsBounds(t *testing.T) {
	atBound := map[string][]string{}
	for page := range maxCollapsedSectionPages {
		sections := make([]string, maxCollapsedSectionsPerPage)
		for i := range sections {
			sections[i] = fmt.Sprintf("%0*d", maxSectionNameBytes, i)
		}
		atBound[fmt.Sprintf("%0*d", maxSectionNameBytes, page)] = sections
	}
	require.NoError(t, ValidateCollapsedSections(atBound))
	require.NoError(t, ValidateCollapsedSections(map[string][]string{}))

	overPages := maps.Clone(atBound)
	overPages["one-more"] = []string{"section"}
	tooManySections := make([]string, maxCollapsedSectionsPerPage+1)
	for i := range tooManySections {
		tooManySections[i] = fmt.Sprint(i)
	}
	tooLong := strings.Repeat("x", maxSectionNameBytes+1)
	for name, collapsed := range map[string]map[string][]string{
		"pages":             overPages,
		"sections":          {"hardware": tooManySections},
		"long page name":    {tooLong: {"sensors"}},
		"long section name": {"hardware": {tooLong}},
		"blank page name":   {" ": {"sensors"}},
		"blank section":     {"hardware": {""}},
	} {
		require.Error(t, ValidateCollapsedSections(collapsed), name)
	}
}
