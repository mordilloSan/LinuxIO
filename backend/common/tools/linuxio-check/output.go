//go:build linux

package main

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// OutputMux gives one task at a time the terminal in a fixed display order.
// Other tasks are spooled to disk until their turn. Replay skips bytes already
// shown live, so output is never duplicated or interleaved.
type OutputMux struct {
	mu        sync.Mutex
	dir       string
	terminal  io.Writer
	files     map[string]*os.File
	fileErr   map[string]error
	displayed map[string]int64
	started   map[string]bool
	done      map[string]bool
	order     []string
	sequence  []string
	index     int
	live      string
	err       error
}

func NewOutputMux(dir string, terminal io.Writer, order ...string) *OutputMux {
	return &OutputMux{dir: dir, terminal: terminal, files: make(map[string]*os.File), fileErr: make(map[string]error), displayed: make(map[string]int64), started: make(map[string]bool), done: make(map[string]bool), order: order}
}

func (m *OutputMux) Start(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[name]; !ok {
		file, err := os.CreateTemp(m.dir, "linuxio-check-*")
		if err != nil {
			wrapped := fmt.Errorf("create output spool for %s: %w", name, err)
			m.fileErr[name] = wrapped
			m.recordErrorLocked(wrapped)
		} else {
			m.files[name] = file
		}
	}
	m.started[name] = true
	m.sequence = append(m.sequence, name)
	m.recordErrorLocked(m.advanceLocked())
}

func (m *OutputMux) Writer(name string) io.Writer {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[name]; ok {
		return &taskOutput{mux: m, name: name}
	}
	if err := m.fileErr[name]; err != nil {
		return errorWriter{err: err}
	}
	return errorWriter{err: fmt.Errorf("output spool for %s was not started", name)}
}

func (m *OutputMux) write(name string, data []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	file := m.files[name]
	if file == nil {
		if err := m.fileErr[name]; err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("output spool for %s is not open", name)
	}
	n, err := file.Write(data)
	if err != nil {
		return n, err
	}
	if name == m.live && m.terminal != nil {
		liveN, liveErr := m.terminal.Write(data[:n])
		m.displayed[name] += int64(liveN)
		if liveErr == nil && liveN != n {
			liveErr = io.ErrShortWrite
		}
		if liveErr != nil {
			m.recordErrorLocked(fmt.Errorf("live output for %s: %w", name, liveErr))
			return liveN, liveErr
		}
	}
	return n, nil
}

func (m *OutputMux) Finish(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done[name] = true
	m.recordErrorLocked(m.advanceLocked())
}

func (m *OutputMux) recordErrorLocked(err error) {
	if err != nil && m.err == nil {
		m.err = err
	}
}

// advanceLocked owns the terminal according to the fixed replay order. A
// completed earlier task is drained before advancing; a running task at the
// current position is followed live. Tasks not yet admitted stop advancement,
// while canceled pending tasks are simply absent from the order's files and
// are skipped by Replay after scheduling completes.
func (m *OutputMux) advanceLocked() error {
	order := m.order
	if len(order) == 0 {
		order = m.sequence
	}
	for m.index < len(order) {
		name := order[m.index]
		if !m.started[name] {
			return nil
		}
		if !m.done[name] {
			if m.live != name {
				if err := m.replayLocked(name); err != nil {
					return err
				}
			}
			m.live = name
			return nil
		}
		if err := m.replayLocked(name); err != nil {
			return err
		}
		m.live = ""
		m.index++
	}
	return nil
}

func (m *OutputMux) replayLocked(name string) error {
	file := m.files[name]
	if file == nil || m.terminal == nil {
		return nil
	}
	if _, err := file.Seek(m.displayed[name], io.SeekStart); err != nil {
		return fmt.Errorf("seek output spool for %s: %w", name, err)
	}
	written, err := io.Copy(m.terminal, file)
	m.displayed[name] += written
	if err != nil {
		return fmt.Errorf("replay output for %s: %w", name, err)
	}
	return nil
}

func (m *OutputMux) Replay(order []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	for _, name := range order {
		file := m.files[name]
		if file == nil {
			continue
		}
		if err := m.replayLocked(name); err != nil {
			return err
		}
	}
	return nil
}

func (m *OutputMux) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var first error
	for name, file := range m.files {
		if err := file.Close(); err != nil && first == nil {
			first = fmt.Errorf("close output spool for %s: %w", name, err)
		}
		if err := os.Remove(file.Name()); err != nil && !os.IsNotExist(err) && first == nil {
			first = fmt.Errorf("remove output spool for %s: %w", name, err)
		}
	}
	if m.dir != "" {
		// Remove only the now-empty spool directory. The caller owns the
		// directory; never recursively remove a caller-provided path.
		if err := os.Remove(m.dir); err != nil && !os.IsNotExist(err) && first == nil {
			first = err
		}
	}
	return first
}

type taskOutput struct {
	mux  *OutputMux
	name string
}

func (w *taskOutput) Write(data []byte) (int, error) { return w.mux.write(w.name, data) }

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }
