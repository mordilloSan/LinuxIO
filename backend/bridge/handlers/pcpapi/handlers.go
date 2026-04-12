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

	commonpcpapi "github.com/mordilloSan/LinuxIO/backend/common/pcpapi"
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
	cfg, err := commonpcpapi.ReadConfig(commonpcpapi.DefaultConfigPath)
	if err != nil {
		return err
	}
	return emit.Result(cfg)
}

func handleSetConfig(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) != 1 {
		return ipc.ErrInvalidArgs
	}

	var cfg commonpcpapi.Config
	if err := json.Unmarshal([]byte(args[0]), &cfg); err != nil {
		return ipc.ErrInvalidArgs
	}
	cfg = commonpcpapi.NormalizeConfig(cfg)

	if err := commonpcpapi.WriteConfig(commonpcpapi.DefaultConfigPath, cfg); err != nil {
		return err
	}

	if err := applyServiceState(cfg); err != nil {
		return err
	}
	logger.Infof("pcp api config updated")
	return emit.Result(cfg)
}

func handleGetStatus(ctx context.Context, args []string, emit ipc.Events) error {
	cfg, cfgErr := commonpcpapi.ReadConfig(commonpcpapi.DefaultConfigPath)
	if cfgErr != nil {
		cfg = commonpcpapi.DefaultConfig()
	}

	activeState, err := systemdapi.GetActiveState(commonpcpapi.ServiceName)
	if err != nil {
		return err
	}
	unitFileState, err := systemdapi.GetUnitFileState(commonpcpapi.ServiceName)
	if err != nil {
		return err
	}

	status := ServiceStatus{
		Unit:          commonpcpapi.ServiceName,
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
	if err := systemdapi.RestartUnit(commonpcpapi.ServiceName); err != nil {
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
	cfg, err := commonpcpapi.ReadConfig(commonpcpapi.DefaultConfigPath)
	if err != nil {
		return err
	}

	token, err := commonpcpapi.GenerateToken()
	if err != nil {
		return err
	}
	err = commonpcpapi.WriteToken(cfg.Auth.TokenFile, token)
	if err != nil {
		return err
	}
	activeState, err := systemdapi.GetActiveState(commonpcpapi.ServiceName)
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
	cfg, err := commonpcpapi.ReadConfig(commonpcpapi.DefaultConfigPath)
	if err != nil {
		return err
	}
	token, err := commonpcpapi.ReadToken(cfg.Auth.TokenFile)
	if err != nil {
		return err
	}
	return emit.Result(TokenResponse{
		Path:  cfg.Auth.TokenFile,
		Token: token,
	})
}

func applyServiceState(cfg commonpcpapi.Config) error {
	if cfg.Enabled {
		if err := systemdapi.EnableUnit(commonpcpapi.ServiceName); err != nil {
			return err
		}
		activeState, err := systemdapi.GetActiveState(commonpcpapi.ServiceName)
		if err != nil {
			return err
		}
		switch activeState {
		case "active":
			return reloadOrRestart()
		default:
			return systemdapi.StartUnit(commonpcpapi.ServiceName)
		}
	}

	if err := systemdapi.StopUnit(commonpcpapi.ServiceName); err != nil {
		return err
	}
	return systemdapi.DisableUnit(commonpcpapi.ServiceName)
}

func reloadOrRestart() error {
	if err := systemdapi.ReloadUnit(commonpcpapi.ServiceName); err == nil {
		return nil
	}
	return systemdapi.RestartUnit(commonpcpapi.ServiceName)
}
