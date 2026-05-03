package docker

import "testing"

func TestExtractHostPortsTreatsTCPAndUDPAsDistinct(t *testing.T) {
	svc := map[string]any{
		"ports": []any{
			"53:53/tcp",
			"53:53/udp",
			map[string]any{
				"target":    67,
				"published": "67",
				"protocol":  "udp",
			},
			map[string]any{
				"target":    80,
				"published": "8080",
			},
		},
	}

	got := extractHostPorts(svc)
	want := []string{"53/tcp", "53/udp", "67/udp", "8080/tcp"}

	if len(got) != len(want) {
		t.Fatalf("extractHostPorts() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("extractHostPorts()[%d] = %q, want %q (all: %#v)", i, got[i], want[i], got)
		}
	}
}

func TestValidateComposeFileAllowsPiHoleDNSProtocols(t *testing.T) {
	content := `
services:
  pihole:
    image: pihole/pihole:latest
    ports:
      - "53:53/tcp"
      - "53:53/udp"
`

	resultAny, err := ValidateComposeFile(content)
	if err != nil {
		t.Fatalf("ValidateComposeFile() error = %v", err)
	}
	result, ok := resultAny.(ValidationResult)
	if !ok {
		t.Fatalf("ValidateComposeFile() type = %T, want ValidationResult", resultAny)
	}
	if !result.Valid {
		t.Fatalf("ValidateComposeFile() valid = false, errors = %#v", result.Errors)
	}
}

func TestValidateComposeFileRejectsDuplicateHostPortProtocol(t *testing.T) {
	content := `
services:
  web:
    image: nginx:latest
    ports:
      - "8080:80/tcp"
      - "8080:8080/tcp"
`

	resultAny, err := ValidateComposeFile(content)
	if err != nil {
		t.Fatalf("ValidateComposeFile() error = %v", err)
	}
	result, ok := resultAny.(ValidationResult)
	if !ok {
		t.Fatalf("ValidateComposeFile() type = %T, want ValidationResult", resultAny)
	}
	if result.Valid {
		t.Fatalf("ValidateComposeFile() valid = true, want duplicate port error")
	}
	if len(result.Errors) == 0 {
		t.Fatalf("ValidateComposeFile() errors empty")
	}
}
