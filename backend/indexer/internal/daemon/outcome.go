package daemon

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// keptOutcomes is how many ended operations the daemon answers for. One is
// asked about right after its stream closed, or after the few pauses of a
// client whose stream broke, so only the latest matter. The reindexes that
// file operations and opened folders start end here too, each in a
// millisecond or more, and a burst of them must not push out the one a
// client is about to ask for: it would be told the operation was lost.
const keptOutcomes = 4096

// outcomeLog holds how the latest index and reindex operations ended. A
// stream that closes says nothing by itself: the operation completed, failed
// before anyone attached, or was lost with a daemon that stopped. Whoever
// followed it asks here, and an operation that is not found was lost.
type outcomeLog struct {
	mu    sync.Mutex
	byID  map[string]workStreamEvent
	order []string
}

// keep records how the operation of stream ended. One that sent no last event
// is kept as failed: nothing says its work was done.
func (l *outcomeLog) keep(stream *workStreamBroadcaster) {
	stream.mu.Lock()
	last := stream.last
	stream.mu.Unlock()
	outcome := workStreamEvent{event: api.EventError, data: api.WorkErrorEvent{
		Status:      api.StatusError,
		Operation:   stream.Operation(),
		OperationID: stream.OperationID(),
		Path:        stream.Path(),
		Message:     "the operation ended without a result",
	}}
	if last != nil {
		outcome = *last
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byID == nil {
		l.byID = make(map[string]workStreamEvent)
	}
	id := stream.OperationID()
	if _, held := l.byID[id]; !held {
		l.order = append(l.order, id)
	}
	l.byID[id] = outcome
	for len(l.order) > keptOutcomes {
		delete(l.byID, l.order[0])
		l.order = l.order[1:]
	}
}

// of returns the last event of the operation with that ID, when it is still
// held.
func (l *outcomeLog) of(operationID string) (workStreamEvent, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	outcome, found := l.byID[operationID]
	return outcome, found
}

// handleOperation answers how an operation that is over ended. It answers
// 404 for one that still waits or runs, and for one this daemon never held.
func (d *daemon) handleOperation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "use GET", http.StatusMethodNotAllowed)
		return
	}
	operationID := strings.TrimSpace(r.URL.Query().Get("operation_id"))
	if operationID == "" {
		http.Error(w, "operation_id is required", http.StatusBadRequest)
		return
	}
	outcome, found := d.outcomes.of(operationID)
	if !found {
		http.Error(w, fmt.Sprintf("no ended operation %q", operationID), http.StatusNotFound)
		return
	}
	// As the event was written to its stream: a path is bytes, not text.
	data, err := json.Marshal(outcome.data, jsontext.AllowInvalidUTF8(true))
	if err != nil {
		slog.Warn("failed to encode an operation's outcome", "operation_id", operationID, "error", err)
		http.Error(w, "the operation's outcome cannot be encoded", http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.OperationOutcome{Event: outcome.event, Data: data})
}
