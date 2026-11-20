package octalescape

import "testing"

func TestUnescape(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"no escape", "/mnt/data", "/mnt/data"},
		{"space in a mount point", `/mnt/my\040disk`, "/mnt/my disk"},
		{"space in a btrfs root", `/my\040share`, "/my share"},
		{"space in a swap file", `/mnt/my\040swap/file`, "/mnt/my swap/file"},
		{"deleted swap file", `/swap.img\040(deleted)`, "/swap.img (deleted)"},
		{"fstab label", `LABEL=my\040data`, "LABEL=my data"},
		{"escape at the end", `/mnt/a\040`, "/mnt/a "},
		{"tab", `a\011b`, "a\tb"},
		{"newline", `a\012b`, "a\nb"},
		{"backslash", `a\134b`, `a\b`},
		{"decoded backslash starts no escape", `a\134040`, `a\040`},
		{"backslash before an escape", `a\134\040`, `a\ `},
		{"other codes up to 377", `\000\101\377`, "\x00A\xff"},
		{"code above 377", `a\400b\777`, `a\400b\777`},
		{"digit that is not octal", `a\09b\8`, `a\09b\8`},
		{"two digits at the end", `a\04`, `a\04`},
		{"trailing backslash", `a\`, `a\`},
		{"lone backslash", `\`, `\`},
		{"doubled backslash", `a\\b`, `a\\b`},
		{"doubled backslash before an escape", `a\\040`, `a\ `},
		{"C escape", `a\nb\t`, `a\nb\t`},
		{"sign, prefix or underscore", `\+12\0x1\0_1`, `\+12\0x1\0_1`},
		{"multibyte text kept", `/mnt/über\040x`, "/mnt/über x"},
	}
	for _, tc := range tests {
		if got := Unescape(tc.in); got != tc.want {
			t.Errorf("%s: Unescape(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}
