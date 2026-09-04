package network

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConnectionStatsJSONContract(t *testing.T) {
	stats := ConnectionStats{
		NetConnectionsEnabled: true,
		NFConntrackEnabled:    true,
		Total:                 12,
		TCP:                   8,
		UDP:                   4,
		Established:           5,
		Statuses:              map[string]int{"ESTABLISHED": 5, "TIME_WAIT": 2},
		NFConntrackCount:      100,
		NFConntrackMax:        200,
		NFConntrackPercent:    50,
	}

	payload, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}

	body := string(payload)
	for _, field := range []string{
		`"net_connections_enabled":true`,
		`"nf_conntrack_enabled":true`,
		`"total":12`,
		`"tcp":8`,
		`"udp":4`,
		`"established":5`,
		`"statuses"`,
		`"nf_conntrack_count":100`,
		`"nf_conntrack_max":200`,
		`"nf_conntrack_percent":50`,
	} {
		if !strings.Contains(body, field) {
			t.Fatalf("marshaled ConnectionStats missing %s in %s", field, body)
		}
	}
	for _, omitted := range []string{
		`"listen"`,
		`"syn_sent"`,
		`"close_wait"`,
	} {
		if strings.Contains(body, omitted) {
			t.Fatalf("marshaled ConnectionStats unexpectedly included %s in %s", omitted, body)
		}
	}

	var decoded ConnectionStats
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Statuses["ESTABLISHED"] != 5 || decoded.NFConntrackPercent != 50 {
		t.Fatalf("decoded ConnectionStats lost status details: %#v", decoded)
	}
}

func TestIRQStatJSONContract(t *testing.T) {
	stat := IRQStat{
		IRQ:         "16",
		Total:       42,
		CPUCounts:   []uint64{10, 32},
		Description: "eth0",
	}

	payload, err := json.Marshal(stat)
	if err != nil {
		t.Fatal(err)
	}

	const want = `{"irq":"16","total":42,"cpu_counts":[10,32],"description":"eth0"}`
	if string(payload) != want {
		t.Fatalf("json.Marshal(IRQStat) = %s, want %s", payload, want)
	}
}

func TestIRQStatJSONOmitsEmptyOptionalFields(t *testing.T) {
	payload, err := json.Marshal(IRQStat{IRQ: "0", Total: 1})
	if err != nil {
		t.Fatal(err)
	}

	body := string(payload)
	if strings.Contains(body, "cpu_counts") || strings.Contains(body, "description") {
		t.Fatalf("marshaled IRQStat included empty optional fields: %s", body)
	}
}
