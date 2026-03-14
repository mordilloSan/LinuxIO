package ipc

import (
	"bytes"
	"encoding/json"
	"testing"
)

type benchmarkProgress struct {
	Bytes int64  `json:"bytes"`
	Total int64  `json:"total"`
	Phase string `json:"phase"`
}

type benchmarkResult struct {
	Name    string            `json:"name"`
	Count   int               `json:"count"`
	Active  bool              `json:"active"`
	Labels  []string          `json:"labels"`
	Metrics map[string]uint64 `json:"metrics"`
}

var (
	benchmarkFrameSink  *StreamFrame
	benchmarkStringSink string
	benchmarkIntSink    int
	benchmarkResultData = benchmarkResult{
		Name:   "linuxio",
		Count:  42,
		Active: true,
		Labels: []string{"bridge", "ipc", "benchmark"},
		Metrics: map[string]uint64{
			"bytes":   1 << 20,
			"streams": 32,
		},
	}
	benchmarkResultBytes, _ = json.Marshal(benchmarkResultData)
)

func BenchmarkWriteRelayFrameSmall(b *testing.B) {
	benchmarkWriteRelayFrame(b, 128)
}

func BenchmarkWriteRelayFrameLarge(b *testing.B) {
	benchmarkWriteRelayFrame(b, 32*1024)
}

func BenchmarkReadRelayFrameSmall(b *testing.B) {
	benchmarkReadRelayFrame(b, 128)
}

func BenchmarkReadRelayFrameLarge(b *testing.B) {
	benchmarkReadRelayFrame(b, 32*1024)
}

func BenchmarkParseStreamOpenPayload(b *testing.B) {
	payload := []byte("bridge\x00filebrowser\x00download\x00/tmp/example.tar.gz\x001048576")

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))

	for i := 0; i < b.N; i++ {
		streamType, args := ParseStreamOpenPayload(payload)
		benchmarkStringSink = streamType
		benchmarkIntSink = len(args)
	}
}

func BenchmarkWriteProgress(b *testing.B) {
	progress := benchmarkProgress{
		Bytes: 512 * 1024,
		Total: 1024 * 1024,
		Phase: "streaming",
	}
	var buf bytes.Buffer

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := WriteProgress(&buf, 7, progress); err != nil {
			b.Fatal(err)
		}
		benchmarkIntSink = buf.Len()
	}
}

func BenchmarkWriteResultFrame(b *testing.B) {
	result := &ResultFrame{
		Status: "ok",
		Data:   benchmarkResultBytes,
	}
	var buf bytes.Buffer

	b.ReportAllocs()
	b.SetBytes(int64(len(benchmarkResultBytes)))

	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := WriteResultFrame(&buf, 9, result); err != nil {
			b.Fatal(err)
		}
		benchmarkIntSink = buf.Len()
	}
}

func BenchmarkWriteResultOK(b *testing.B) {
	var buf bytes.Buffer

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := WriteResultOK(&buf, 11, benchmarkResultData); err != nil {
			b.Fatal(err)
		}
		benchmarkIntSink = buf.Len()
	}
}

func benchmarkWriteRelayFrame(b *testing.B, payloadSize int) {
	payload := bytes.Repeat([]byte("x"), payloadSize)
	frame := &StreamFrame{
		Opcode:   OpStreamData,
		StreamID: 1,
		Payload:  payload,
	}
	var buf bytes.Buffer

	b.ReportAllocs()
	b.SetBytes(int64(len(payload) + 9))

	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := WriteRelayFrame(&buf, frame); err != nil {
			b.Fatal(err)
		}
		benchmarkIntSink = buf.Len()
	}
}

func benchmarkReadRelayFrame(b *testing.B, payloadSize int) {
	payload := bytes.Repeat([]byte("x"), payloadSize)
	encoded := mustEncodeBenchmarkFrame(b, &StreamFrame{
		Opcode:   OpStreamData,
		StreamID: 1,
		Payload:  payload,
	})
	var reader bytes.Reader

	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))

	for i := 0; i < b.N; i++ {
		reader.Reset(encoded)
		frame, err := ReadRelayFrame(&reader)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkFrameSink = frame
	}
}

func mustEncodeBenchmarkFrame(b *testing.B, frame *StreamFrame) []byte {
	b.Helper()

	var buf bytes.Buffer
	if err := WriteRelayFrame(&buf, frame); err != nil {
		b.Fatal(err)
	}
	return buf.Bytes()
}
