package system

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type ServiceInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
	MainPID     int32  `json:"main_pid"`
	Failed      bool   `json:"failed"`
}

func FetchServices() ([]ServiceInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl",
		"show", "--type=service", "--all", "--no-pager",
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
		if info.Name == "" || !strings.HasSuffix(info.Name, ".service") {
			continue
		}
		info.Failed = (info.ActiveState == "failed" || info.SubState == "failed")
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
