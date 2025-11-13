package iteminfo

// Editable text file extensions
var editableTextTypes = map[string]bool{
	".txt":   true,
	".md":    true,
	".sh":    true,
	".py":    true,
	".js":    true,
	".ts":    true,
	".jsx":   true,
	".tsx":   true,
	".php":   true,
	".rb":    true,
	".go":    true,
	".java":  true,
	".c":     true,
	".cpp":   true,
	".cs":    true,
	".swift": true,
	".yaml":  true,
	".yml":   true,
	".json":  true,
	".xml":   true,
	".ini":   true,
	".toml":  true,
	".cfg":   true,
	".css":   true,
	".html":  true,
	".htm":   true,
	".sql":   true,
	".csv":   true,
	".log":   true,
	".bash":  true,
	".zsh":   true,
	".fish":  true,
}

// IsEditableText checks if a file extension is editable as text
func IsEditableText(ext string) bool {
	return editableTextTypes[ext]
}
