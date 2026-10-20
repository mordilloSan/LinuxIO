package virt

import (
	"context"
	"testing"
	"time"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func statsRecord(name string, cpuNS, rx, rd uint64) libvirt.DomainStatsRecord {
	return libvirt.DomainStatsRecord{Dom: testDomain(name), Params: []libvirt.TypedParam{
		{Field: "cpu.time", Value: *libvirt.NewTypedParamValueUllong(cpuNS)},
		{Field: "balloon.rss", Value: *libvirt.NewTypedParamValueUllong(512 * 1024)},
		{Field: "net.count", Value: *libvirt.NewTypedParamValueUint(2)},
		{Field: "net.0.name", Value: *libvirt.NewTypedParamValueString("vnet0")},
		{Field: "net.0.rx.bytes", Value: *libvirt.NewTypedParamValueUllong(rx)},
		{Field: "net.1.name", Value: *libvirt.NewTypedParamValueString("vnet1")},
		{Field: "net.1.rx.bytes", Value: *libvirt.NewTypedParamValueUllong(rx)},
		{Field: "block.count", Value: *libvirt.NewTypedParamValueUint(1)},
		{Field: "block.0.rd.bytes", Value: *libvirt.NewTypedParamValueUllong(rd)},
	}}
}

func TestEnrichMetricsComputesRatesFromSecondSample(t *testing.T) {
	fake := newFakeConn()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base
	statsNow = func() time.Time { return now }
	t.Cleanup(func() { statsNow = time.Now })
	domains := []libvirt.Domain{testDomain("vm"), testDomain("off")}
	vms := []apischema.VirtualMachine{{Name: "vm", UUID: "uuid-vm", VCPUs: 2, NICs: []apischema.VMNIC{{Target: "vnet1"}, {Target: "vnet9"}}}, {Name: "off"}}

	fake.domainStats = []libvirt.DomainStatsRecord{statsRecord("vm", 1e9, 1000, 0), {Dom: testDomain("off")}}
	enrichMetrics(fake, domains, vms)
	if vms[1].Metrics != nil {
		t.Fatalf("shut-off VM got metrics: %#v", vms[1].Metrics)
	}
	got := vms[0].Metrics
	if got == nil || got.MemoryUsedMB != 512 || got.CPUPercent != nil || got.NetworkReceiveBytesPerSecond != nil {
		t.Fatalf("first sample = %#v, want memory only", got)
	}

	// Two seconds later the domain used one full second of CPU across two vCPUs.
	now = base.Add(2 * time.Second)
	fake.domainStats = []libvirt.DomainStatsRecord{statsRecord("vm", 2e9, 5000, 4096)}
	enrichMetrics(fake, domains, vms)
	got = vms[0].Metrics
	if got == nil || got.CPUPercent == nil || *got.CPUPercent != 25 {
		t.Fatalf("CPUPercent = %#v, want 25", got)
	}
	if got.NetworkReceiveBytesPerSecond == nil || *got.NetworkReceiveBytesPerSecond != 4000 {
		t.Fatalf("NetworkReceiveBytesPerSecond = %#v, want 4000 (both NICs summed)", got.NetworkReceiveBytesPerSecond)
	}
	if got.DiskReadBytesPerSecond == nil || *got.DiskReadBytesPerSecond != 2048 {
		t.Fatalf("DiskReadBytesPerSecond = %#v, want 2048", got.DiskReadBytesPerSecond)
	}
	if nic := vms[0].NICs[0]; nic.ReceiveBytesPerSecond == nil || *nic.ReceiveBytesPerSecond != 2000 || nic.SendBytesPerSecond == nil || *nic.SendBytesPerSecond != 0 {
		t.Fatalf("NIC vnet1 rates = %#v, want 2000 rx / 0 tx", nic)
	}
	if nic := vms[0].NICs[1]; nic.ReceiveBytesPerSecond != nil {
		t.Fatalf("NIC vnet9 (not in stats) rates = %#v, want none", nic)
	}
	if got.NetworkSendBytesPerSecond == nil || *got.NetworkSendBytesPerSecond != 0 {
		t.Fatalf("NetworkSendBytesPerSecond = %#v, want 0", got.NetworkSendBytesPerSecond)
	}

	// A restarted domain reports smaller counters: no rate rather than a negative one.
	now = base.Add(4 * time.Second)
	fake.domainStats = []libvirt.DomainStatsRecord{statsRecord("vm", 1e8, 10, 0)}
	enrichMetrics(fake, domains, vms)
	if vms[0].Metrics == nil || vms[0].Metrics.CPUPercent != nil {
		t.Fatalf("after counter reset = %#v, want no rates", vms[0].Metrics)
	}
}

func TestListStoragePools(t *testing.T) {
	fake := newFakeConn()
	fake.pools = []libvirt.StoragePool{{Name: "zeta"}, {Name: "default"}}
	fake.poolXML = map[string]string{
		"default": `<pool type="dir"><name>default</name><target><path>/var/lib/libvirt/images</path></target></pool>`,
		"zeta":    `<pool type="dir"><name>zeta</name><target><path>/srv/zeta</path></target></pool>`,
	}
	fake.poolInfo = map[string][3]uint64{"default": {1000, 400, 600}}
	fake.poolActiveByName = map[string]int32{"default": 1, "zeta": 0}
	withFakeLibvirt(t, fake)

	pools, err := ListStoragePools(context.Background())
	if err != nil {
		t.Fatalf("ListStoragePools: %v", err)
	}
	if len(pools) != 2 || pools[0].Name != "default" || pools[1].Name != "zeta" {
		t.Fatalf("pools = %#v, want sorted by name", pools)
	}
	got := pools[0]
	if !got.Active || got.Type != "dir" || got.Path != "/var/lib/libvirt/images" || got.CapacityBytes != 1000 || got.AvailableBytes != 600 {
		t.Fatalf("default pool = %#v", got)
	}
	if pools[1].Active || pools[1].CapacityBytes != 0 {
		t.Fatalf("inactive pool = %#v, want no sizes", pools[1])
	}
}
