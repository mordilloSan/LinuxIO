package userconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
	colors, err := NewThemeColors(map[string]*string{"chartRx": new("#123456")}, "dark")
	require.NoError(t, err)
	require.Equal(t, CSSColor("#123456"), *colors.ChartRx)
	require.Nil(t, colors.ChartTx)

	colors, err = NewThemeColors(map[string]*string{}, "dark")
	require.NoError(t, err)
	require.Nil(t, colors)

	_, err = NewThemeColors(map[string]*string{"codeText": new("nope")}, "light")
	require.EqualError(t, err, "invalid themeColors.light.codeText")
}

func TestThemeColorSlotsCoverEveryField(t *testing.T) {
	colors := &ThemeColors{}
	require.Len(t, themeColorSlots(colors), 18)
	cloned := cloneThemeColors(defaultThemeColors().Dark)
	for _, slot := range themeColorSlots(cloned) {
		require.NotNil(t, *slot.Value, slot.Key)
	}
}

func TestValidateJobSettingsReportsNegatives(t *testing.T) {
	require.Empty(t, ValidateJobSettings(DefaultJobSettings()))
	require.Equal(t, []string{"jobs.progressMinBytesMB must be >= 0"}, ValidateJobSettings(PersistedJobSettings{ProgressMinBytesMB: -1}))
}
