package network

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetplanSetIPv4ManualWritesConfigAndApplies(t *testing.T) {
	env, runner, _ := testEnv(t)
	path := filepath.Join(env.NetplanDir, "01-eth0.yaml")
	mustWriteFile(t, path, `
network:
  version: 2
  ethernets:
    eth0:
      dhcp4: true
      nameservers:
        addresses: [2001:4860:4860::8888]
`)
	backend, err := detectNetplanBackend(env, "eth0")
	if err != nil {
		t.Fatalf("detectNetplanBackend: %v", err)
	}
	setErr := backend.SetIPv4Manual(context.Background(), "192.168.10.50/24", "192.168.10.1", []string{"1.1.1.1", "8.8.8.8"})
	if setErr != nil {
		t.Fatalf("SetIPv4Manual: %v", setErr)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated netplan: %v", err)
	}
	body := string(updated)
	for _, want := range []string{
		"dhcp4: false",
		"192.168.10.50/24",
		"via: 192.168.10.1",
		"- 1.1.1.1",
		"- 8.8.8.8",
		"- 2001:4860:4860::8888",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in netplan file:\n%s", want, body)
		}
	}
	requireCalls(t, runner, "netplan-dbus Generate", "netplan-dbus Apply")
}

func TestNetplanSetOptionalRegeneratesWithoutApply(t *testing.T) {
	env, runner, _ := testEnv(t)
	path := filepath.Join(env.NetplanDir, "01-eth0.yaml")
	mustWriteFile(t, path, `
network:
  version: 2
  ethernets:
    eth0:
      dhcp4: true
`)
	backend, err := detectNetplanBackend(env, "eth0")
	if err != nil {
		t.Fatalf("detectNetplanBackend: %v", err)
	}
	setter, ok := backend.(OptionalSetter)
	if !ok {
		t.Fatalf("%T does not implement OptionalSetter", backend)
	}
	for _, optional := range []bool{true, false} {
		runner.calls = nil
		if setErr := setter.SetOptional(context.Background(), optional); setErr != nil {
			t.Fatalf("SetOptional(%v): %v", optional, setErr)
		}
		requireCalls(t, runner, "netplan-dbus Generate")
		cfg, readErr := backend.Read()
		if readErr != nil {
			t.Fatalf("Read: %v", readErr)
		}
		if cfg.Optional == nil || *cfg.Optional != optional {
			t.Fatalf("Optional = %v, want %v", cfg.Optional, optional)
		}
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated netplan: %v", err)
	}
	if strings.Contains(string(updated), "optional") {
		t.Fatalf("clearing optional should drop the key:\n%s", updated)
	}
}

func TestNetplanDHCPWithStaticDNSIgnoresDHCPServers(t *testing.T) {
	env, _, _ := testEnv(t)
	path := filepath.Join(env.NetplanDir, "01-eth0.yaml")
	mustWriteFile(t, path, `
network:
  version: 2
  ethernets:
    eth0:
      dhcp4: false
      addresses: [192.168.1.20/24]
      routes:
        - to: default
          via: 192.168.1.1
      nameservers:
        addresses: [8.8.8.8, 2001:4860:4860::8888]
`)
	backend, err := detectNetplanBackend(env, "eth0")
	if err != nil {
		t.Fatalf("detectNetplanBackend: %v", err)
	}
	setter, ok := backend.(DNSOptionsSetter)
	if !ok {
		t.Fatalf("%T does not implement DNSOptionsSetter", backend)
	}
	if setErr := setter.SetIPv4DHCPWithDNS(context.Background(), []string{"192.168.1.66"}, []string{"lan", "home.arpa"}); setErr != nil {
		t.Fatalf("SetIPv4DHCPWithDNS: %v", setErr)
	}
	cfg, err := backend.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if cfg.IPv4Method != "auto" || cfg.Gateway != "" || len(cfg.IPv4Addresses) != 0 {
		t.Fatalf("expected plain DHCP, got %+v", cfg)
	}
	if strings.Join(cfg.DNS, ",") != "2001:4860:4860::8888,192.168.1.66" {
		t.Fatalf("DNS = %v", cfg.DNS)
	}
	if cfg.DNSOptions == nil || !cfg.DNSOptions.IgnoreDHCP || strings.Join(cfg.DNSOptions.Search, ",") != "lan,home.arpa" {
		t.Fatalf("DNSOptions = %+v", cfg.DNSOptions)
	}

	if setErr := setter.SetIPv4DHCPWithDNS(context.Background(), nil, nil); setErr != nil {
		t.Fatalf("SetIPv4DHCPWithDNS(nil): %v", setErr)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated netplan: %v", err)
	}
	for _, gone := range []string{"dhcp4-overrides", "search", "192.168.1.66"} {
		if strings.Contains(string(updated), gone) {
			t.Fatalf("expected %q removed:\n%s", gone, updated)
		}
	}
}

func TestNetplanManualWithSearchClearsDHCPDNSOverride(t *testing.T) {
	env, _, _ := testEnv(t)
	path := filepath.Join(env.NetplanDir, "01-eth0.yaml")
	mustWriteFile(t, path, `
network:
  version: 2
  ethernets:
    eth0:
      dhcp4: true
      dhcp4-overrides:
        use-dns: false
        route-metric: 50
`)
	backend, err := detectNetplanBackend(env, "eth0")
	if err != nil {
		t.Fatalf("detectNetplanBackend: %v", err)
	}
	setter, ok := backend.(DNSOptionsSetter)
	if !ok {
		t.Fatalf("%T does not implement DNSOptionsSetter", backend)
	}
	setErr := setter.SetIPv4ManualWithSearch(context.Background(), "10.0.0.5/24", "10.0.0.1", []string{"10.0.0.1"}, []string{"corp.example"})
	if setErr != nil {
		t.Fatalf("SetIPv4ManualWithSearch: %v", setErr)
	}
	cfg, err := backend.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if cfg.DNSOptions.IgnoreDHCP || strings.Join(cfg.DNSOptions.Search, ",") != "corp.example" {
		t.Fatalf("DNSOptions = %+v", cfg.DNSOptions)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated netplan: %v", err)
	}
	if !strings.Contains(string(updated), "route-metric: 50") {
		t.Fatalf("unrelated DHCP override dropped:\n%s", updated)
	}
}

func TestNetplanDisablePersistsAndEnableClears(t *testing.T) {
	env, runner, _ := testEnv(t)
	// No such link exists, so only the persisted half of each call succeeds.
	const iface = "lio-missing0"
	path := filepath.Join(env.NetplanDir, "01-test.yaml")
	mustWriteFile(t, path, `
network:
  version: 2
  ethernets:
    lio-missing0:
      dhcp4: true
`)
	backend, err := detectNetplanBackend(env, iface)
	if err != nil {
		t.Fatalf("detectNetplanBackend: %v", err)
	}
	if disableErr := backend.Disable(context.Background()); disableErr == nil {
		t.Fatal("Disable of a missing link should fail at link down")
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read netplan: %v", err)
	}
	if !strings.Contains(string(updated), "activation-mode: \"off\"") && !strings.Contains(string(updated), "activation-mode: off") {
		t.Fatalf("expected activation-mode off:\n%s", updated)
	}
	requireCalls(t, runner, "netplan-dbus Generate")

	runner.calls = nil
	if enableErr := backend.Enable(context.Background()); enableErr == nil {
		t.Fatal("Enable of a missing link should fail at link up")
	}
	updated, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read netplan: %v", err)
	}
	if strings.Contains(string(updated), "activation-mode") {
		t.Fatalf("expected activation-mode removed:\n%s", updated)
	}
	requireCalls(t, runner, "netplan-dbus Generate", "netplan-dbus Apply")
}

func TestNetworkdSetIPv4ManualUsesReloadAndReconfigure(t *testing.T) {
	env, runner, _ := testEnv(t)
	path := filepath.Join(env.NetworkdDir, "10-eth0.network")
	mustWriteFile(t, path, `
[Match]
Name=eth0

[Network]
DHCP=yes
DNS=2001:4860:4860::8888
`)
	backend, err := detectNetworkdBackend(env, "eth0")
	if err != nil {
		t.Fatalf("detectNetworkdBackend: %v", err)
	}
	setErr := backend.SetIPv4Manual(context.Background(), "10.0.0.20/24", "10.0.0.1", []string{"9.9.9.9"})
	if setErr != nil {
		t.Fatalf("SetIPv4Manual: %v", setErr)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated networkd file: %v", err)
	}
	body := string(updated)
	for _, want := range []string{
		"DHCP=ipv6",
		"Address=10.0.0.20/24",
		"Gateway=10.0.0.1",
		"DNS=2001:4860:4860::8888",
		"DNS=9.9.9.9",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in networkd file:\n%s", want, body)
		}
	}
	requireCalls(t, runner, "networkctl reload", "networkctl reconfigure eth0")
}

func TestIfupdownSetIPv4ManualRewritesBlockAndRunsIfdownIfup(t *testing.T) {
	env, runner, _ := testEnv(t)
	path := filepath.Join(env.IfupdownDir, "eth0")
	mustWriteFile(t, path, `
auto eth0
iface eth0 inet dhcp
	mtu 1500
`)
	backend, err := detectIfupdownBackend(env, "eth0")
	if err != nil {
		t.Fatalf("detectIfupdownBackend: %v", err)
	}
	setErr := backend.SetIPv4Manual(context.Background(), "172.16.0.10/24", "172.16.0.1", []string{"1.1.1.1"})
	if setErr != nil {
		t.Fatalf("SetIPv4Manual: %v", setErr)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated interfaces file: %v", err)
	}
	body := string(updated)
	for _, want := range []string{
		"iface eth0 inet static",
		"address 172.16.0.10/24",
		"gateway 172.16.0.1",
		"dns-nameservers 1.1.1.1",
		"mtu 1500",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in interfaces file:\n%s", want, body)
		}
	}
	requireCalls(t, runner, "ifdown eth0", "ifup eth0")
}

func TestIfcfgSetIPv4ManualWritesExpectedKeysAndRunsIfup(t *testing.T) {
	env, runner, _ := testEnv(t)
	path := filepath.Join(env.IfcfgDir, "ifcfg-eth0")
	mustWriteFile(t, path, `
DEVICE=eth0
BOOTPROTO=dhcp
ONBOOT=yes
`)
	backend, err := detectIfcfgBackend(env, "eth0")
	if err != nil {
		t.Fatalf("detectIfcfgBackend: %v", err)
	}
	setErr := backend.SetIPv4Manual(context.Background(), "192.168.1.20/24", "192.168.1.1", []string{"8.8.8.8", "1.1.1.1"})
	if setErr != nil {
		t.Fatalf("SetIPv4Manual: %v", setErr)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated ifcfg file: %v", err)
	}
	body := string(updated)
	for _, want := range []string{
		"BOOTPROTO=none",
		"IPADDR=192.168.1.20",
		"PREFIX=24",
		"GATEWAY=192.168.1.1",
		"DNS1=8.8.8.8",
		"DNS2=1.1.1.1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in ifcfg file:\n%s", want, body)
		}
	}
	requireCalls(t, runner, "ifdown eth0", "ifup eth0")
}

func TestNMConnectionSetIPv4ManualWritesKeyfileAndUsesNmcli(t *testing.T) {
	env, runner, _ := testEnv(t)
	path := filepath.Join(env.NMConnectionDir, "eth0.nmconnection")
	mustWriteFile(t, path, `
[connection]
id=eth0
uuid=11111111-2222-3333-4444-555555555555
type=802-3-ethernet
interface-name=eth0

[ipv4]
method=auto

[ipv6]
method=auto

[ethernet]
mtu=1500
`)
	backend, err := detectNMConnectionBackend(env, "eth0")
	if err != nil {
		t.Fatalf("detectNMConnectionBackend: %v", err)
	}
	setErr := backend.SetIPv4Manual(context.Background(), "192.168.50.10/24", "192.168.50.1", []string{"4.4.4.4"})
	if setErr != nil {
		t.Fatalf("SetIPv4Manual: %v", setErr)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated nmconnection: %v", err)
	}
	body := string(updated)
	for _, want := range []string{
		"method=manual",
		"address1=192.168.50.10/24",
		"gateway=192.168.50.1",
		"dns=4.4.4.4;",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in nmconnection file:\n%s", want, body)
		}
	}
	requireCalls(
		t,
		runner,
		"nmcli connection load "+path,
		"nmcli device reapply eth0",
	)
}

func TestNMConnectionReapplyFallsBackToConnectionUp(t *testing.T) {
	env, runner, _ := testEnv(t)
	path := filepath.Join(env.NMConnectionDir, "eth0.nmconnection")
	mustWriteFile(t, path, `
[connection]
id=eth0
uuid=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee
type=802-3-ethernet
interface-name=eth0

[ipv4]
method=auto
`)
	runner.fail("nmcli device reapply eth0", errors.New("boom"))
	backend, err := detectNMConnectionBackend(env, "eth0")
	if err != nil {
		t.Fatalf("detectNMConnectionBackend: %v", err)
	}
	if err := backend.SetIPv4DHCP(context.Background()); err != nil {
		t.Fatalf("SetIPv4DHCP: %v", err)
	}
	requireCalls(
		t,
		runner,
		"nmcli connection load "+path,
		"nmcli device reapply eth0",
		"nmcli connection up uuid aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
	)
}
