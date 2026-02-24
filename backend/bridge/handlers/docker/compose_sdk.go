package docker

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	composeapi "github.com/docker/compose/v5/pkg/api"
	composepkg "github.com/docker/compose/v5/pkg/compose"
	"github.com/mordilloSan/go-logger/logger"
)

type composeLineEmitter func(msgType, message string)

type composeMessageCollector struct {
	mu    sync.Mutex
	lines []string
}

func (c *composeMessageCollector) Emit(_ string, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}

	c.mu.Lock()
	c.lines = append(c.lines, message)
	c.mu.Unlock()
}

func (c *composeMessageCollector) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.lines, "\n")
}

type composeEventProcessor struct {
	emit composeLineEmitter
}

func (p *composeEventProcessor) Start(_ context.Context, operation string) {
	if p == nil || p.emit == nil {
		return
	}
	p.emit("stdout", fmt.Sprintf("%s started", strings.ToLower(operation)))
}

func (p *composeEventProcessor) On(events ...composeapi.Resource) {
	if p == nil || p.emit == nil {
		return
	}

	for _, event := range events {
		message := formatComposeEvent(event)
		if message == "" {
			continue
		}
		p.emit(composeEventType(event.Status), message)
	}
}

func (p *composeEventProcessor) Done(operation string, success bool) {
	if p == nil || p.emit == nil {
		return
	}

	if success {
		p.emit("stdout", fmt.Sprintf("%s completed", strings.ToLower(operation)))
		return
	}
	p.emit("stderr", fmt.Sprintf("%s failed", strings.ToLower(operation)))
}

func composeEventType(status composeapi.EventStatus) string {
	switch status {
	case composeapi.Warning, composeapi.Error:
		return "stderr"
	default:
		return "stdout"
	}
}

func formatComposeEvent(event composeapi.Resource) string {
	parts := make([]string, 0, 3)

	switch {
	case event.ID != "" && event.Text != "":
		parts = append(parts, fmt.Sprintf("%s: %s", event.ID, event.Text))
	case event.ID != "":
		parts = append(parts, event.ID)
	case event.Text != "":
		parts = append(parts, event.Text)
	}

	if event.Details != "" {
		parts = append(parts, event.Details)
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, " ")
}

func newComposeSDKService(emitter composeLineEmitter) (composeapi.Compose, func(), error) {
	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return nil, nil, fmt.Errorf("create docker CLI: %w", err)
	}

	clientOptions := flags.NewClientOptions()
	if err = dockerCLI.Initialize(clientOptions); err != nil {
		return nil, nil, fmt.Errorf("initialize docker CLI: %w", err)
	}

	options := []composepkg.Option{
		composepkg.WithOutputStream(io.Discard),
		composepkg.WithErrorStream(io.Discard),
		composepkg.WithPrompt(composepkg.AlwaysOkPrompt()),
	}
	if emitter != nil {
		options = append(options, composepkg.WithEventProcessor(&composeEventProcessor{emit: emitter}))
	}

	composeService, err := composepkg.NewComposeService(dockerCLI, options...)
	if err != nil {
		return nil, nil, fmt.Errorf("create compose service: %w", err)
	}

	cleanup := func() {
		if closer, ok := composeService.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				logger.Debugf("failed to close compose service: %v", err)
			}
		}
	}

	return composeService, cleanup, nil
}

func composeUpWithSDK(
	ctx context.Context,
	projectName, configFile, workingDir string,
	removeOrphans bool,
	emitter composeLineEmitter,
) error {
	composeService, cleanup, err := newComposeSDKService(emitter)
	if err != nil {
		return err
	}
	defer cleanup()

	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}

	project, err := composeService.LoadProject(ctx, composeapi.ProjectLoadOptions{
		ProjectName: projectName,
		ConfigPaths: []string{configFile},
		WorkingDir:  workingDir,
	})
	if err != nil {
		return fmt.Errorf("load compose project: %w", err)
	}

	return composeService.Up(ctx, project, composeapi.UpOptions{
		Create: composeapi.CreateOptions{
			RemoveOrphans: removeOrphans,
		},
		Start: composeapi.StartOptions{
			Project: project,
		},
	})
}

func composeDownWithSDK(
	ctx context.Context,
	projectName, configFile, workingDir string,
	removeOrphans bool,
	emitter composeLineEmitter,
) error {
	composeService, cleanup, err := newComposeSDKService(emitter)
	if err != nil {
		return err
	}
	defer cleanup()

	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}

	project, err := composeService.LoadProject(ctx, composeapi.ProjectLoadOptions{
		ProjectName: projectName,
		ConfigPaths: []string{configFile},
		WorkingDir:  workingDir,
	})
	if err != nil {
		return fmt.Errorf("load compose project: %w", err)
	}

	return composeService.Down(ctx, projectName, composeapi.DownOptions{
		Project:       project,
		RemoveOrphans: removeOrphans,
	})
}

func composeStopWithSDK(
	ctx context.Context,
	projectName, configFile, workingDir string,
	emitter composeLineEmitter,
) error {
	composeService, cleanup, err := newComposeSDKService(emitter)
	if err != nil {
		return err
	}
	defer cleanup()

	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}

	project, err := composeService.LoadProject(ctx, composeapi.ProjectLoadOptions{
		ProjectName: projectName,
		ConfigPaths: []string{configFile},
		WorkingDir:  workingDir,
	})
	if err != nil {
		return fmt.Errorf("load compose project: %w", err)
	}

	return composeService.Stop(ctx, projectName, composeapi.StopOptions{
		Project: project,
	})
}

func composeValidateContentWithSDK(ctx context.Context, content string) error {
	configDetails := types.ConfigDetails{
		WorkingDir: ".",
		ConfigFiles: []types.ConfigFile{{
			Filename: "compose.yml",
			Content:  []byte(content),
		}},
		Environment: map[string]string{},
	}

	_, err := loader.LoadWithContext(ctx, configDetails, func(opts *loader.Options) {
		opts.SetProjectName("linuxio-validate", true)
	})
	return err
}
