package web

import (
	"bytes"
	"io"
	"testing"
	"time"
)

type benchmarkWSMessageWriter struct {
	sink     io.Writer
	deadline time.Time
}

type nopWriteCloser struct {
	io.Writer
}

func (w *benchmarkWSMessageWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}

func (w *benchmarkWSMessageWriter) NextWriter(int) (io.WriteCloser, error) {
	return nopWriteCloser{Writer: w.sink}, nil
}

func (nopWriteCloser) Close() error {
	return nil
}

var websocketBenchIntSink int

func BenchmarkWriteBinaryFrameMessageSmall(b *testing.B) {
	benchmarkWriteBinaryFrameMessage(b, 128)
}

func BenchmarkWriteBinaryFrameMessageLarge(b *testing.B) {
	benchmarkWriteBinaryFrameMessage(b, relayReadBufferSize)
}

func benchmarkWriteBinaryFrameMessage(b *testing.B, payloadSize int) {
	payload := bytes.Repeat([]byte("x"), payloadSize)
	var sink bytes.Buffer
	sink.Grow(payloadSize + 5)
	writer := &benchmarkWSMessageWriter{sink: &sink}
	deadline := time.Unix(0, 0)

	b.ReportAllocs()
	b.SetBytes(int64(payloadSize + 5))

	for i := 0; i < b.N; i++ {
		sink.Reset()
		if err := writeBinaryFrameMessage(writer, deadline, 7, FlagDATA, payload); err != nil {
			b.Fatal(err)
		}
		websocketBenchIntSink = sink.Len()
	}
}
