package config

import (
	"testing"
)

func TestLookupEnv(t *testing.T) {
	t.Setenv(EnvMemCalc, "htop")
	if value, ok := LookupEnv(EnvMemCalc); !ok || value != "htop" {
		t.Fatalf("LookupEnv(%s) = %q, %v", EnvMemCalc, value, ok)
	}
	if value, ok := LookupEnv("LINUXIO_MONITORING_TEST_UNSET"); ok || value != "" {
		t.Fatalf("unset variable = %q, %v", value, ok)
	}
}
