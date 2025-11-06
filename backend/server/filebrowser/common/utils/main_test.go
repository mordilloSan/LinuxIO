package utils

import "testing"

func TestGetParentDirectoryPath(t *testing.T) {
	tests := []struct {
		input          string
		expectedOutput string
	}{
		{input: "/", expectedOutput: ""},                                              // Root directory
		{input: "/subfolder", expectedOutput: "/"},                                    // Single subfolder
		{input: "/sub/sub/", expectedOutput: "/sub"},                                  // Nested subfolder with trailing slash
		{input: "/subfolder/", expectedOutput: "/"},                                   // Relative path with trailing slash
		{input: "", expectedOutput: ""},                                               // Empty string treated as root
		{input: "/sub/subfolder", expectedOutput: "/sub"},                             // Double slash in path
		{input: "/sub/subfolder/deep/nested/", expectedOutput: "/sub/subfolder/deep"}, // Double slash in path
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			actualOutput := GetParentDirectoryPath(test.input)
			if actualOutput != test.expectedOutput {
				t.Errorf("\n\tinput %q\n\texpected %q\n\tgot %q",
					test.input, test.expectedOutput, actualOutput)
			}
		})
	}
}
