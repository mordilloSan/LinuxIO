package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateConfigRejectsUnknownACLTargets(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Exposure.Categories["nope"] = ExposurePublic
	cfg.Exposure.Endpoints["/api/v1/nope"] = ExposurePrivate

	errs := ValidateConfig(cfg)

	require.Len(t, errs, 2)
	require.Contains(t, errs[0], "nope")
	require.Contains(t, errs[1], "/api/v1/nope")
}

func TestEndpointPolicyPrefersEndpointOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Exposure.Categories["cpu"] = ExposurePublic
	cfg.Exposure.Endpoints["/api/v1/cpu"] = ExposurePrivate

	policy := EndpointPolicy(cfg, "/api/v1/cpu")

	require.Equal(t, ExposurePrivate, policy)
}

func TestWriteAndReadConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pcp-api.yaml")

	cfg := DefaultConfig()
	cfg.ListenAddress = "0.0.0.0:9000"
	cfg.Exposure.Categories["cpu"] = ExposurePublic

	require.NoError(t, WriteConfig(path, cfg))

	loaded, err := ReadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:9000", loaded.ListenAddress)
	require.Equal(t, ExposurePublic, loaded.Exposure.Categories["cpu"])
}

func TestWriteAndReadTokenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pcp-api.token")

	require.NoError(t, WriteToken(path, "secret-token"))

	token, err := ReadToken(path)
	require.NoError(t, err)
	require.Equal(t, "secret-token", token)
}
