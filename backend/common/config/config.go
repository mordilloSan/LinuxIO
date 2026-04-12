package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
)

const (
	ServiceName          = "linuxio-pcp-api.service"
	DefaultConfigPath    = "/etc/linuxio/pcp-api.yaml"
	DefaultTokenPath     = "/etc/linuxio/pcp-api.token"
	DefaultListenAddress = "127.0.0.1:8091"

	configDirPerm  = 0o755
	configFilePerm = 0o640
	tokenFilePerm  = 0o600
)

type ExposurePolicy string

const (
	ExposurePublic  ExposurePolicy = "public"
	ExposurePrivate ExposurePolicy = "private"
)

var (
	Categories = []string{
		"summary",
		"cpu",
		"memory",
		"network",
		"disk",
		"filesystems",
		"thermal",
		"system",
	}
	EndpointCategory = map[string]string{
		"/api/v1/summary":     "summary",
		"/api/v1/cpu":         "cpu",
		"/api/v1/memory":      "memory",
		"/api/v1/network":     "network",
		"/api/v1/disk":        "disk",
		"/api/v1/filesystems": "filesystems",
		"/api/v1/thermal":     "thermal",
		"/api/v1/system":      "system",
	}
)

type AuthConfig struct {
	Enabled   bool   `json:"enabled" yaml:"enabled"`
	TokenFile string `json:"token_file" yaml:"token_file"`
}

type ExposureConfig struct {
	Categories map[string]ExposurePolicy `json:"categories" yaml:"categories"`
	Endpoints  map[string]ExposurePolicy `json:"endpoints" yaml:"endpoints"`
}

type Config struct {
	Enabled       bool           `json:"enabled" yaml:"enabled"`
	ListenAddress string         `json:"listen_address" yaml:"listen_address"`
	Auth          AuthConfig     `json:"auth" yaml:"auth"`
	Exposure      ExposureConfig `json:"exposure" yaml:"exposure"`
}

func DefaultConfig() Config {
	return Config{
		Enabled:       false,
		ListenAddress: DefaultListenAddress,
		Auth: AuthConfig{
			Enabled:   true,
			TokenFile: DefaultTokenPath,
		},
		Exposure: ExposureConfig{
			Categories: map[string]ExposurePolicy{
				"summary": ExposurePublic,
			},
			Endpoints: map[string]ExposurePolicy{},
		},
	}
}

func NormalizeConfig(cfg Config) Config {
	defaults := DefaultConfig()

	if strings.TrimSpace(cfg.ListenAddress) == "" {
		cfg.ListenAddress = defaults.ListenAddress
	}
	if strings.TrimSpace(cfg.Auth.TokenFile) == "" {
		cfg.Auth.TokenFile = defaults.Auth.TokenFile
	}
	if cfg.Exposure.Categories == nil {
		cfg.Exposure.Categories = map[string]ExposurePolicy{}
	}
	if cfg.Exposure.Endpoints == nil {
		cfg.Exposure.Endpoints = map[string]ExposurePolicy{}
	}

	return cfg
}

func ValidateConfig(cfg Config) []string {
	cfg = NormalizeConfig(cfg)

	var errs []string
	if strings.TrimSpace(cfg.ListenAddress) == "" {
		errs = append(errs, "listen_address is required")
	}
	if strings.TrimSpace(cfg.Auth.TokenFile) == "" {
		errs = append(errs, "auth.token_file is required")
	}

	for category, policy := range cfg.Exposure.Categories {
		if !slices.Contains(Categories, category) {
			errs = append(errs, fmt.Sprintf("exposure.categories.%s is not a known category", category))
			continue
		}
		if !validPolicy(policy) {
			errs = append(errs, fmt.Sprintf("exposure.categories.%s must be public or private", category))
		}
	}

	for endpoint, policy := range cfg.Exposure.Endpoints {
		if _, ok := EndpointCategory[endpoint]; !ok {
			errs = append(errs, fmt.Sprintf("exposure.endpoints.%s is not a known endpoint", endpoint))
			continue
		}
		if !validPolicy(policy) {
			errs = append(errs, fmt.Sprintf("exposure.endpoints.%s must be public or private", endpoint))
		}
	}

	return errs
}

func EndpointPolicy(cfg Config, endpoint string) ExposurePolicy {
	cfg = NormalizeConfig(cfg)

	if policy, ok := cfg.Exposure.Endpoints[endpoint]; ok && validPolicy(policy) {
		return policy
	}
	if category, ok := EndpointCategory[endpoint]; ok {
		if policy, ok := cfg.Exposure.Categories[category]; ok && validPolicy(policy) {
			return policy
		}
	}
	return ExposurePrivate
}

func IsEndpointPublic(cfg Config, endpoint string) bool {
	return EndpointPolicy(cfg, endpoint) == ExposurePublic
}

func ReadConfig(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := DefaultConfig()
	dec := yaml.NewDecoder(bytes.NewReader(data), yaml.Strict())
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, err
	}

	cfg = NormalizeConfig(cfg)
	if errs := ValidateConfig(cfg); len(errs) > 0 {
		return Config{}, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return cfg, nil
}

func WriteConfig(path string, cfg Config) error {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath
	}

	cfg = NormalizeConfig(cfg)
	if errs := ValidateConfig(cfg); len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, configFilePerm)
}

func ReadToken(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultTokenPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return token, nil
}

func WriteToken(path, token string) error {
	if strings.TrimSpace(path) == "" {
		path = DefaultTokenPath
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}
	return writeFileAtomic(path, []byte(token+"\n"), tokenFilePerm)
}

func GenerateToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func EnsureDefaultFiles(configPath, tokenPath string) (Config, string, error) {
	if strings.TrimSpace(configPath) == "" {
		configPath = DefaultConfigPath
	}
	if strings.TrimSpace(tokenPath) == "" {
		tokenPath = DefaultTokenPath
	}

	if _, err := os.Stat(tokenPath); err != nil {
		if !os.IsNotExist(err) {
			return Config{}, "", err
		}
		token, tokenErr := GenerateToken()
		if tokenErr != nil {
			return Config{}, "", tokenErr
		}
		if writeErr := WriteToken(tokenPath, token); writeErr != nil {
			return Config{}, "", writeErr
		}
	}

	if _, err := os.Stat(configPath); err != nil {
		if !os.IsNotExist(err) {
			return Config{}, "", err
		}
		cfg := DefaultConfig()
		cfg.Auth.TokenFile = tokenPath
		if writeErr := WriteConfig(configPath, cfg); writeErr != nil {
			return Config{}, "", writeErr
		}
	}

	cfg, err := ReadConfig(configPath)
	if err != nil {
		return Config{}, "", err
	}
	token, err := ReadToken(cfg.Auth.TokenFile)
	if err != nil {
		return Config{}, "", err
	}
	return cfg, token, nil
}

func validPolicy(policy ExposurePolicy) bool {
	return policy == ExposurePublic || policy == ExposurePrivate
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Chmod(path, perm)
}
