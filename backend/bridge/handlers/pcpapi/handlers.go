package pcpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/privilege"
	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
)

type registration struct {
	command string
	handler ipc.HandlerFunc
}

type ServiceStatus struct {
	Unit          string `json:"unit"`
	ActiveState   string `json:"active_state"`
	UnitFileState string `json:"unit_file_state"`
	Enabled       bool   `json:"enabled"`
	ConfigEnabled bool   `json:"config_enabled"`
	ListenAddress string `json:"listen_address"`
	Healthy       bool   `json:"healthy"`
	HealthError   string `json:"health_error,omitempty"`
	Version       string `json:"version,omitempty"`
}

type TokenResponse struct {
	Path  string `json:"path"`
	Token string `json:"token"`
}

func RegisterHandlers(sess *session.Session) {
	register := []registration{
		{command: "get_config", handler: handleGetConfig},
		{command: "set_config", handler: handleSetConfig},
		{command: "get_status", handler: handleGetStatus},
		{command: "restart_service", handler: handleRestartService},
		{command: "reload_service", handler: handleReloadService},
		{command: "rotate_token", handler: handleRotateToken},
		{command: "get_token", handler: handleGetToken},
	}

	for _, item := range register {
		ipc.RegisterFunc("pcp_api", item.command, privilege.RequirePrivilegedIPC(sess, item.handler))
	}
}

func handleGetConfig(ctx context.Context, args []string, emit ipc.Events) error {
	cfg, err := config.ReadConfig(config.DefaultConfigPath)
	if err != nil {
		return err
	}
	return emit.Result(cfg)
}

func handleSetConfig(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) != 1 {
		return ipc.ErrInvalidArgs
	}

	var cfg config.Config
	if err := json.Unmarshal([]byte(args[0]), &cfg); err != nil {
		return ipc.ErrInvalidArgs
	}
	cfg = config.NormalizeConfig(cfg)

	if err := config.WriteConfig(config.DefaultConfigPath, cfg); err != nil {
		return err
	}

	if err := applyServiceState(cfg); err != nil {
		return err
	}
	logger.Infof("pcp api config updated")
	return emit.Result(cfg)
}

func handleGetStatus(ctx context.Context, args []string, emit ipc.Events) error {
	cfg, cfgErr := config.ReadConfig(config.DefaultConfigPath)
	if cfgErr != nil {
		cfg = config.DefaultConfig()
	}

	activeState, err := systemdapi.GetActiveState(config.ServiceName)
	if err != nil {
		return err
	}
	unitFileState, err := systemdapi.GetUnitFileState(config.ServiceName)
	if err != nil {
		return err
	}

	status := ServiceStatus{
		Unit:          config.ServiceName,
		ActiveState:   activeState,
		UnitFileState: unitFileState,
		Enabled:       strings.HasPrefix(unitFileState, "enabled"),
		ConfigEnabled: cfg.Enabled,
		ListenAddress: cfg.ListenAddress,
	}

	healthURL := fmt.Sprintf("http://%s/healthz", cfg.ListenAddress)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, reqErr := client.Get(healthURL)
	if reqErr != nil {
		status.HealthError = reqErr.Error()
		return emit.Result(status)
	}
	defer resp.Body.Close()

	var payload struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(body, &payload); err != nil {
		status.HealthError = err.Error()
		return emit.Result(status)
	}

	status.Healthy = resp.StatusCode == http.StatusOK && payload.OK
	status.Version = payload.Version
	return emit.Result(status)
}

func handleRestartService(ctx context.Context, args []string, emit ipc.Events) error {
	if err := systemdapi.RestartUnit(config.ServiceName); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleReloadService(ctx context.Context, args []string, emit ipc.Events) error {
	if err := reloadOrRestart(); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleRotateToken(ctx context.Context, args []string, emit ipc.Events) error {
	cfg, err := config.ReadConfig(config.DefaultConfigPath)
	if err != nil {
		return err
	}

	token, err := config.GenerateToken()
	if err != nil {
		return err
	}
	err = config.WriteToken(cfg.Auth.TokenFile, token)
	if err != nil {
		return err
	}
	activeState, err := systemdapi.GetActiveState(config.ServiceName)
	if err != nil {
		return err
	}
	if activeState == "active" {
		if err := reloadOrRestart(); err != nil {
			return err
		}
	}

	return emit.Result(TokenResponse{
		Path:  cfg.Auth.TokenFile,
		Token: token,
	})
}

func handleGetToken(ctx context.Context, args []string, emit ipc.Events) error {
	cfg, err := config.ReadConfig(config.DefaultConfigPath)
	if err != nil {
		return err
	}
	token, err := config.ReadToken(cfg.Auth.TokenFile)
	if err != nil {
		return err
	}
	return emit.Result(TokenResponse{
		Path:  cfg.Auth.TokenFile,
		Token: token,
	})
}

func applyServiceState(cfg config.Config) error {
	if cfg.Enabled {
		if err := systemdapi.EnableUnit(config.ServiceName); err != nil {
			return err
		}
		activeState, err := systemdapi.GetActiveState(config.ServiceName)
		if err != nil {
			return err
		}
		switch activeState {
		case "active":
			return reloadOrRestart()
		default:
			return systemdapi.StartUnit(config.ServiceName)
		}
	}

	if err := systemdapi.StopUnit(config.ServiceName); err != nil {
		return err
	}
	return systemdapi.DisableUnit(config.ServiceName)
}

func reloadOrRestart() error {
	if err := systemdapi.ReloadUnit(config.ServiceName); err == nil {
		return nil
	}
	return systemdapi.RestartUnit(config.ServiceName)
}
