package daemon

import (
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// Fixed daemon policy. None of these values is an operator setting; the idle
// timings match the webserver's.
const (
	// idleGrace is how long the daemon stays up with no request, event stream,
	// scan or webserver marker before it exits. The socket reactivates it.
	idleGrace         = 90 * time.Second
	idleCheckInterval = 15 * time.Second

	// runtimeDirectoryMarker is created by linuxio-webserver.service and keeps
	// the daemon warm while the webserver runs.
	runtimeDirectoryMarker = version.RunDir + "/webserver"

	// maxSearchQueryBytes bounds the raw q parameter. Together with
	// searchquery.MaxTerms it caps the per-request scan cost of a search.
	maxSearchQueryBytes = 256
	searchResultLimit   = 100

	// maxRequestBodyBytes bounds request bodies on /add and /config.
	maxRequestBodyBytes = 1 << 20

	// retainedGenerations is how many published generations survive a scan.
	retainedGenerations = 1

	// startupProbeTimeout bounds the journal-mode probe at startup.
	startupProbeTimeout = 5 * time.Second

	// writerBufferEntries sizes the channel between traversal and the SQL
	// writer; storage batches the actual writes.
	writerBufferEntries = 1000

	// scanProgressInterval is how often the full scan samples its counters.
	scanProgressInterval = 500 * time.Millisecond

	// reindexProgressStride emits one reindex progress event per this many
	// entries written.
	reindexProgressStride = 100

	// workerKillDelay is how long a worker with malformed output or a cancelled
	// context gets to exit after SIGTERM before it is killed.
	workerKillDelay = 5 * time.Second

	// sseWriteDeadline bounds each individual event write. Setting it per write
	// replaces the server's absolute WriteTimeout for this connection, which
	// would otherwise cut every stream long before a full index or vacuum
	// finishes, while still timing out writes to dead clients.
	sseWriteDeadline = 30 * time.Second

	// workStreamSubscriberBuffer is the per-subscriber event backlog before a
	// slow subscriber is dropped.
	workStreamSubscriberBuffer = 64

	httpReadHeaderTimeout = 10 * time.Second
	httpReadTimeout       = 30 * time.Second
	httpIdleTimeout       = 60 * time.Second
	httpMaxHeaderBytes    = 64 << 10
)
