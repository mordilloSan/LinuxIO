package process

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProcessJSONContract(t *testing.T) {
	proc := Process{
		PID:           1234,
		Name:          "postgres",
		Cmdline:       []string{"postgres", "-D", "/var/lib/postgresql"},
		Username:      "postgres",
		Status:        "sleep",
		NumThreads:    7,
		CPUPercent:    12.5,
		MemoryPercent: 3.25,
		MemoryInfo: MemoryInfo{
			RSS:  1024,
			VMS:  4096,
			HWM:  2048,
			Swap: 128,
		},
		IOCounters: IOCounters{
			ReadBytes:               10,
			WriteBytes:              20,
			DiskReadBytes:           30,
			DiskWriteBytes:          40,
			DiskReadBytesPerSecond:  5,
			DiskWriteBytesPerSecond: 6,
		},
		ContainerID:   "abcdef123456",
		ContainerName: "database",
	}

	payload, err := json.Marshal(proc)
	if err != nil {
		t.Fatal(err)
	}

	body := string(payload)
	for _, field := range []string{
		`"pid":1234`,
		`"name":"postgres"`,
		`"cmdline":["postgres","-D","/var/lib/postgresql"]`,
		`"username":"postgres"`,
		`"status":"sleep"`,
		`"num_threads":7`,
		`"cpu_percent":12.5`,
		`"memory_percent":3.25`,
		`"memory_info":{"rss":1024,"vms":4096,"hwm":2048`,
		`"swap":128`,
		`"io_counters":{"read_bytes":10,"write_bytes":20,"disk_read_bytes":30,"disk_write_bytes":40,"disk_read_bytes_per_second":5,"disk_write_bytes_per_second":6}`,
		`"container_id":"abcdef123456"`,
		`"container_name":"database"`,
	} {
		if !strings.Contains(body, field) {
			t.Fatalf("marshaled Process missing %s in %s", field, body)
		}
	}
	if strings.Contains(body, `"data"`) || strings.Contains(body, `"stack"`) || strings.Contains(body, `"locked"`) {
		t.Fatalf("marshaled Process included empty optional memory fields: %s", body)
	}

	var decoded Process
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PID != proc.PID || decoded.MemoryInfo.Swap != proc.MemoryInfo.Swap || decoded.IOCounters.DiskWriteBytesPerSecond != proc.IOCounters.DiskWriteBytesPerSecond || decoded.ContainerID != proc.ContainerID {
		t.Fatalf("decoded Process lost payload values: %#v", decoded)
	}
}

func TestProcessJSONKeepsRequiredZeroValueFields(t *testing.T) {
	payload, err := json.Marshal(Process{Name: "idle"})
	if err != nil {
		t.Fatal(err)
	}

	body := string(payload)
	for _, field := range []string{
		`"pid":0`,
		`"name":"idle"`,
		`"cpu_percent":0`,
		`"memory_percent":0`,
		`"memory_info":{"rss":0,"vms":0}`,
		`"io_counters":{}`,
	} {
		if !strings.Contains(body, field) {
			t.Fatalf("marshaled Process missing required zero-value field %s in %s", field, body)
		}
	}
}

func TestCountJSONContract(t *testing.T) {
	count := Count{
		Total:    100,
		Running:  3,
		Sleeping: 90,
		Zombie:   1,
		Thread:   220,
		PIDMax:   4194304,
	}

	payload, err := json.Marshal(count)
	if err != nil {
		t.Fatal(err)
	}

	body := string(payload)
	for _, field := range []string{
		`"total":100`,
		`"running":3`,
		`"sleeping":90`,
		`"zombie":1`,
		`"thread":220`,
		`"pid_max":4194304`,
	} {
		if !strings.Contains(body, field) {
			t.Fatalf("marshaled Count missing %s in %s", field, body)
		}
	}
	for _, omitted := range []string{`"stopped"`, `"blocked"`, `"idle"`} {
		if strings.Contains(body, omitted) {
			t.Fatalf("marshaled Count unexpectedly included %s in %s", omitted, body)
		}
	}
}

func TestProgramJSONContract(t *testing.T) {
	program := Program{
		Name:           "nginx",
		Count:          4,
		CPUPercent:     20.25,
		MemoryPercent:  6.5,
		MemoryRSSBytes: 8192,
		PIDs:           []int32{10, 11, 12, 13},
	}

	payload, err := json.Marshal(program)
	if err != nil {
		t.Fatal(err)
	}

	const want = `{"name":"nginx","count":4,"cpu_percent":20.25,"memory_percent":6.5,"memory_rss_bytes":8192,"pids":[10,11,12,13]}`
	if string(payload) != want {
		t.Fatalf("json.Marshal(Program) = %s, want %s", payload, want)
	}
}
