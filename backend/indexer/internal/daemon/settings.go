package daemon

import (
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
)

// settings holds the operator settings the next scan uses. The same value is
// what GET /config returns and what is persisted to path.
type settings struct {
	mu   sync.RWMutex
	cfg  configfile.Config
	path string // config file written by PUT /config; fixed at startup
}

func (s *settings) config() configfile.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *settings) set(cfg configfile.Config) {
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
}

func (d *daemon) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, d.settings.config())
	case http.MethodPut:
		d.handleConfigPut(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "use GET or PUT", http.StatusMethodNotAllowed)
	}
}

func (d *daemon) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	if !requestFromUnixSocket(r) {
		http.Error(w, "config writes require the Unix socket", http.StatusForbidden)
		return
	}
	if d.running.Load() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}
	patch, err := configfile.DecodePatchJSON(body)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid config JSON: %v", err), http.StatusBadRequest)
		return
	}
	next, err := configfile.ApplyPatch(d.settings.config(), patch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if saveErr := configfile.Save(d.settings.path, next); saveErr != nil {
		http.Error(w, fmt.Sprintf("write config file: %v", saveErr), http.StatusInternalServerError)
		return
	}
	d.settings.set(next)
	writeJSON(w, next)
}
