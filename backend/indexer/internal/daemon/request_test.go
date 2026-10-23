package daemon

import "testing"

func TestValidateRelativePath(t *testing.T) {
	valid := []string{"/", "/home/user", "notes..txt", "/a/b..c/d", "/a/.hidden", "a/./b"}
	for _, p := range valid {
		if !validateRelativePath(p) {
			t.Errorf("validateRelativePath(%q) = false, want true", p)
		}
	}
	invalid := []string{"..", "/..", "../etc", "/a/../b", "a/.."}
	for _, p := range invalid {
		if validateRelativePath(p) {
			t.Errorf("validateRelativePath(%q) = true, want false", p)
		}
	}
}
