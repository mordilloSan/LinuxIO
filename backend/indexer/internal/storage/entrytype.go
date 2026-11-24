package storage

// The type column holds one of these. The scanner and the API name the types
// in words; only the rows are numbers.
const (
	typeFile      = 0
	typeDirectory = 1
)

func typeCode(name string) int {
	if name == "directory" {
		return typeDirectory
	}
	return typeFile
}

func typeName(code int) string {
	if code == typeDirectory {
		return "directory"
	}
	return "file"
}
