package system

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/process"
)

// ---------- Services (systemd) ----------

type ServiceInfo struct {
	Name        string  `json:"name"`         // e.g. "nginx.service"
	Description string  `json:"description"`  // from systemd
	ActiveState string  `json:"active_state"` // "active" | "inactive" | "failed" | ...
	SubState    string  `json:"sub_state"`    // "running" | "dead" | "failed" | ...
	MainPID     int32   `json:"main_pid"`     // 0 if none
	Failed      bool    `json:"failed"`       // ActiveState/SubState == "failed"
	CPUPercent  float64 `json:"cpu_percent,omitempty"`
	MemPercent  float32 `json:"mem_percent,omitempty"`
}

// FetchServices queries systemd without requiring DBus libs (uses `systemctl show`).
// If enrichWithProcessStats is true, it will also attach CPU/MEM for MainPID (if any).
func FetchServices(ctx context.Context, enrichWithProcessStats bool) ([]ServiceInfo, error) {
	// Ensure a timeout
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "systemctl",
		"show",
		"--type=service",
		"--all",
		"--no-pager",
		"--property=Id,ActiveState,SubState,MainPID,Description",
	)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, errors.New("systemctl returned an error; system may not be running systemd")
		}
		return nil, err
	}

	blocks := bytes.Split(out, []byte("\n\n"))
	services := make([]ServiceInfo, 0, len(blocks))

	for _, b := range blocks {
		if len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		info := parseServiceBlock(b)

		// Skip non-services or empty entries
		if info.Name == "" || !strings.HasSuffix(info.Name, ".service") {
			continue
		}

		info.Failed = (info.ActiveState == "failed" || info.SubState == "failed")

		if enrichWithProcessStats && info.MainPID > 0 {
			if p, err := process.NewProcess(info.MainPID); err == nil {
				if cpu, err := p.CPUPercent(); err == nil {
					info.CPUPercent = cpu
				}
				if mem, err := p.MemoryPercent(); err == nil {
					info.MemPercent = mem
				}
			}
		}

		services = append(services, info)
	}
	return services, nil
}

func parseServiceBlock(b []byte) ServiceInfo {
	var s ServiceInfo
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		k := line[:eq]
		v := line[eq+1:]

		switch k {
		case "Id":
			s.Name = v
		case "ActiveState":
			s.ActiveState = v
		case "SubState":
			s.SubState = v
		case "Description":
			s.Description = v
		case "MainPID":
			if n, err := strconv.ParseInt(v, 10, 32); err == nil {
				s.MainPID = int32(n)
			}
		}
	}
	return s
}

// Gin handler
func getServices(c *gin.Context) {
	// Optional: `?enrich=true|1` to add CPU/MEM of MainPID
	enrichQ := strings.ToLower(strings.TrimSpace(c.Query("enrich")))
	enrich := enrichQ == "1" || enrichQ == "true" || enrichQ == "yes"

	list, err := FetchServices(c.Request.Context(), enrich)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list services", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}
