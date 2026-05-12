package network

import (
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
	setErr := backend.SetIPv4Manual("192.168.10.50/24", "192.168.10.1", []string{"1.1.1.1", "8.8.8.8"})
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
	requireCalls(t, runner, "netplan generate", "netplan apply")
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
	setErr := backend.SetIPv4Manual("10.0.0.20/24", "10.0.0.1", []string{"9.9.9.9"})
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
	setErr := backend.SetIPv4Manual("172.16.0.10/24", "172.16.0.1", []string{"1.1.1.1"})
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
	setErr := backend.SetIPv4Manual("192.168.1.20/24", "192.168.1.1", []string{"8.8.8.8", "1.1.1.1"})
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
	setErr := backend.SetIPv4Manual("192.168.50.10/24", "192.168.50.1", []string{"4.4.4.4"})
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
	runner.fail("nmcli device reapply eth0", errBoom())
	backend, err := detectNMConnectionBackend(env, "eth0")
	if err != nil {
		t.Fatalf("detectNMConnectionBackend: %v", err)
	}
	if err := backend.SetIPv4DHCP(); err != nil {
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
