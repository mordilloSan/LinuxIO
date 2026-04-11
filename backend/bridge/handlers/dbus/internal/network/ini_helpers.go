package network

import (
	"strings"

	ini "gopkg.in/ini.v1"
)

func loadINI(data []byte) (*ini.File, error) {
	return ini.LoadSources(ini.LoadOptions{
		AllowShadows:        true,
		IgnoreInlineComment: true,
		AllowBooleanKeys:    true,
	}, data)
}

func renderINI(cfg *ini.File) ([]byte, error) {
	var builder strings.Builder
	wroteSection := false
	for _, section := range cfg.Sections() {
		if skipINISection(section) {
			continue
		}
		if wroteSection {
			builder.WriteByte('\n')
		}
		wroteSection = true
		writeINISection(&builder, section)
	}
	return []byte(strings.TrimRight(builder.String(), "\n")), nil
}

func skipINISection(section *ini.Section) bool {
	return section.Name() == ini.DefaultSection && len(section.Keys()) == 0
}

func writeINISection(builder *strings.Builder, section *ini.Section) {
	if section.Name() != ini.DefaultSection {
		builder.WriteString("[")
		builder.WriteString(section.Name())
		builder.WriteString("]\n")
	}
	for _, key := range section.Keys() {
		writeINIKey(builder, key)
	}
}

func writeINIKey(builder *strings.Builder, key *ini.Key) {
	values := key.ValueWithShadows()
	if len(values) == 0 {
		values = []string{key.String()}
	}
	for _, value := range values {
		builder.WriteString(key.Name())
		if value != "" || key.Value() != "" {
			builder.WriteString("=")
			builder.WriteString(value)
		}
		builder.WriteByte('\n')
	}
}

func setShadowValues(section *ini.Section, key string, values []string) {
	section.DeleteKey(key)
	if len(values) == 0 {
		return
	}
	first, err := section.NewKey(key, values[0])
	if err != nil {
		return
	}
	for _, value := range values[1:] {
		_ = first.AddShadow(value)
	}
}

func sectionShadowValues(section *ini.Section, key string) []string {
	k, err := section.GetKey(key)
	if err != nil {
		return nil
	}
	values := k.ValueWithShadows()
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func deletePrefixedKeys(section *ini.Section, prefix string) {
	for _, key := range section.Keys() {
		if strings.HasPrefix(key.Name(), prefix) {
			section.DeleteKey(key.Name())
		}
	}
}

func maybeDeleteEmptySection(cfg *ini.File, name string) {
	section, err := cfg.GetSection(name)
	if err != nil {
		return
	}
	if len(section.Keys()) == 0 {
		cfg.DeleteSection(name)
	}
}
