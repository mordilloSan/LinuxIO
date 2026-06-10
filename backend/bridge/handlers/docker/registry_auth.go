package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/moby/moby/api/types/registry"
)

type dockerConfigFile struct {
	Auths       map[string]dockerConfigAuth `json:"auths"`
	CredHelpers map[string]string           `json:"credHelpers"`
	CredsStore  string                      `json:"credsStore"`
}

type dockerConfigAuth struct {
	Auth          string `json:"auth"`
	Email         string `json:"email"`
	IdentityToken string `json:"identitytoken"`
	Password      string `json:"password"`
	Username      string `json:"username"`
}

type credentialHelperResponse struct {
	Secret    string `json:"Secret"`
	ServerURL string `json:"ServerURL"`
	Username  string `json:"Username"`
}

func resolveRegistryAuth(ctx context.Context, imageRef string) (string, error) {
	host := registryHostForImageRef(imageRef)
	if host == "" {
		return "", nil
	}

	cfg, err := loadDockerConfig()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	for _, key := range dockerConfigKeysForHost(host) {
		if auth, ok := cfg.Auths[key]; ok {
			return encodeDockerAuth(auth.toRegistryAuthConfig(key))
		}
	}

	helper := cfg.CredHelpers[host]
	if helper == "" {
		helper = cfg.CredsStore
	}
	if helper == "" {
		return "", nil
	}
	for _, key := range dockerConfigKeysForHost(host) {
		auth, err := resolveCredentialHelperAuth(ctx, helper, key)
		if err == nil {
			return encodeDockerAuth(auth)
		}
	}
	return "", nil
}

func registryHostForImageRef(imageRef string) string {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return ""
	}
	host := reference.Domain(named)
	if host == "docker.io" {
		return "docker.io"
	}
	return host
}

func dockerConfigKeysForHost(host string) []string {
	if host == "docker.io" || host == "index.docker.io" {
		return []string{
			"https://index.docker.io/v1/",
			"index.docker.io",
			"docker.io",
			"registry-1.docker.io",
		}
	}
	return []string{host, "https://" + host, "http://" + host}
}

func loadDockerConfig() (*dockerConfigFile, error) {
	configDir := strings.TrimSpace(os.Getenv("DOCKER_CONFIG"))
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configDir = filepath.Join(home, ".docker")
	}
	data, err := os.ReadFile(filepath.Join(configDir, "config.json"))
	if err != nil {
		return nil, err
	}
	var cfg dockerConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Auths == nil {
		cfg.Auths = map[string]dockerConfigAuth{}
	}
	if cfg.CredHelpers == nil {
		cfg.CredHelpers = map[string]string{}
	}
	return &cfg, nil
}

func (a dockerConfigAuth) toRegistryAuthConfig(serverAddress string) registry.AuthConfig {
	authConfig := registry.AuthConfig{
		IdentityToken: a.IdentityToken,
		Password:      a.Password,
		ServerAddress: serverAddress,
		Username:      a.Username,
	}
	if a.Auth != "" && (authConfig.Username == "" || authConfig.Password == "") {
		if decoded, err := base64.StdEncoding.DecodeString(a.Auth); err == nil {
			if username, password, ok := strings.Cut(string(decoded), ":"); ok {
				authConfig.Username = username
				authConfig.Password = password
			}
		}
	}
	return authConfig
}

func resolveCredentialHelperAuth(ctx context.Context, helper, serverURL string) (registry.AuthConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker-credential-"+helper, "get")
	cmd.Stdin = strings.NewReader(serverURL)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return registry.AuthConfig{}, fmt.Errorf("credential helper %s failed: %w: %s", helper, err, strings.TrimSpace(stderr.String()))
	}
	var resp credentialHelperResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return registry.AuthConfig{}, err
	}
	auth := registry.AuthConfig{
		Password:      resp.Secret,
		ServerAddress: serverURL,
		Username:      resp.Username,
	}
	if auth.Username == "<token>" {
		auth.IdentityToken = auth.Password
		auth.Password = ""
		auth.Username = ""
	}
	return auth, nil
}

func encodeDockerAuth(auth registry.AuthConfig) (string, error) {
	if auth.Username == "" && auth.Password == "" && auth.IdentityToken == "" {
		return "", nil
	}
	data, err := json.Marshal(auth)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(data), nil
}
