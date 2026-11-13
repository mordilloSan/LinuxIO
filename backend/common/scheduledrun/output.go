package scheduledrun

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

// headTailWriter keeps the first and the last half of jobs.MaxOutputBytes of
// what it is given and counts the bytes between them.
type headTailWriter struct {
	half    int
	head    []byte
	tail    []byte
	skipped int
}

func newHeadTailWriter() *headTailWriter { return &headTailWriter{half: jobs.MaxOutputBytes / 2} }

func (w *headTailWriter) Write(p []byte) (int, error) {
	n := len(p)
	if room := w.half - len(w.head); room > 0 {
		k := min(room, len(p))
		w.head = append(w.head, p[:k]...)
		p = p[k:]
	}
	w.tail = append(w.tail, p...)
	// Trimming only when the tail doubles keeps the copying amortised.
	if len(w.tail) > 2*w.half {
		w.trim()
	}
	return n, nil
}

func (w *headTailWriter) trim() {
	if drop := len(w.tail) - w.half; drop > 0 {
		w.skipped += drop
		w.tail = append(w.tail[:0], w.tail[drop:]...)
	}
}

// Bytes is the output kept, with a line saying how much was left out.
func (w *headTailWriter) Bytes() []byte {
	w.trim()
	out := append([]byte(nil), w.head...)
	if w.skipped > 0 {
		out = fmt.Appendf(out, "\n[… %d bytes left out …]\n", w.skipped)
	}
	return append(out, w.tail...)
}

// journalOutput writes the journal lines of one invocation, in the journal's
// short-iso format, to w. --sync makes sure the lines the unit has just
// written are readable; --all keeps lines with carriage returns, colour codes
// or invalid UTF-8 instead of replacing them with "[N blob data]".
func journalOutput(ctx context.Context, invocationID string, w io.Writer) error {
	if err := runJournalctl(ctx, io.Discard, "--sync"); err != nil {
		return err
	}
	return runJournalctl(ctx, w, "--no-pager", "--quiet", "--all", "-o", "short-iso", "_SYSTEMD_INVOCATION_ID="+invocationID)
}

func runJournalctl(ctx context.Context, stdout io.Writer, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = ctxErr
	}
	command := "journalctl " + strings.Join(args, " ")
	if message := strings.TrimSpace(stderr.String()); message != "" {
		return fmt.Errorf("%s: %w: %s", command, err, message)
	}
	return fmt.Errorf("%s: %w", command, err)
}
