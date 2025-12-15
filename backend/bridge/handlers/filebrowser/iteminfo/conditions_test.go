package iteminfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsEditableText(t *testing.T) {
	// Test cases where IsEditableText should return true
	editableTrueTestCases := []string{
		".txt",
		".md",
		".sh",
		".py",
		".js",
		".ts",
		".jsx",
		".tsx",
		".php",
		".rb",
		".go",
		".java",
		".c",
		".cpp",
		".cs",
		".swift",
		".yaml",
		".yml",
		".json",
		".xml",
		".ini",
		".toml",
		".cfg",
		".css",
		".html",
		".htm",
		".sql",
		".csv",
		".log",
		".bash",
		".zsh",
		".fish",
	}

	for _, ext := range editableTrueTestCases {
		assert.True(t, IsEditableText(ext), "Expected %s to be editable text", ext)
	}

	// Test cases where IsEditableText should return false
	editableFalseTestCases := []string{
		".mp4",
		".png",
		".jpg",
		".zip",
		".rar",
		".pdf",
		".doc",
		".exe",
	}

	for _, ext := range editableFalseTestCases {
		assert.False(t, IsEditableText(ext), "Expected %s to not be editable text", ext)
	}
}
