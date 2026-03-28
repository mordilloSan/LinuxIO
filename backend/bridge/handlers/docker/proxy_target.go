package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/mordilloSan/go-logger/logger"
)

type ProxyTarget struct {
	URL string `json:"url"`
}

func ResolveProxyTarget(name string) (*ProxyTarget, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	targetURL, err := resolveProxyTargetURL(context.Background(), cli, name)
	if err != nil {
		return nil, err
	}

	return &ProxyTarget{URL: targetURL}, nil
}

func resolveProxyTargetURL(ctx context.Context, cli *client.Client, name string) (string, error) {
	info, err := cli.ContainerInspect(ctx, name)
	if err != nil {
		resolvedID, resolveErr := resolveContainerIDForProxyTarget(ctx, cli, name)
		if resolveErr != nil {
			return "", fmt.Errorf("inspect container: %w (proxy alias resolution failed: %v)", err, resolveErr)
		}
		info, err = cli.ContainerInspect(ctx, resolvedID)
	}
	if err != nil {
		return "", fmt.Errorf("inspect container: %w", err)
	}

	port, ok := info.Config.Labels[ProxyPortLabel]
	if !ok || port == "" {
		return "", fmt.Errorf("label %s not set", ProxyPortLabel)
	}

	ip := ""
	if nw, found := info.NetworkSettings.Networks[linuxIONetworkName]; found {
		ip = nw.IPAddress
	}
	if ip == "" {
		return "", fmt.Errorf("container not connected to %s network", linuxIONetworkName)
	}

	return fmt.Sprintf("http://%s:%s", ip, port), nil
}

func resolveContainerIDForProxyTarget(ctx context.Context, cli *client.Client, name string) (string, error) {
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return "", fmt.Errorf("list containers: %w", err)
	}

	return findContainerIDForProxyTarget(name, containers)
}

func findContainerIDForProxyTarget(name string, containers []container.Summary) (string, error) {
	var (
		exactMatches   []string
		serviceMatches []string
	)

	for _, ctr := range containers {
		if matchesProxyContainerName(name, ctr.Names) {
			exactMatches = append(exactMatches, ctr.ID)
			continue
		}
		if ctr.Labels["com.docker.compose.service"] == name {
			serviceMatches = append(serviceMatches, ctr.ID)
		}
	}

	switch {
	case len(exactMatches) == 1:
		return exactMatches[0], nil
	case len(exactMatches) > 1:
		return "", fmt.Errorf("multiple containers matched name %q", name)
	case len(serviceMatches) == 1:
		return serviceMatches[0], nil
	case len(serviceMatches) > 1:
		return "", fmt.Errorf("multiple compose services matched %q", name)
	default:
		return "", errors.New("container not found")
	}
}

func matchesProxyContainerName(name string, names []string) bool {
	for _, candidate := range names {
		if strings.TrimPrefix(candidate, "/") == name {
			return true
		}
	}
	return false
}
