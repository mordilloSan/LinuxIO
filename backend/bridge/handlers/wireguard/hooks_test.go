package wireguard

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// hookTestLines gives each hook kind its own distinct two-entry value.
func hookTestLines(kind apischema.WireGuardHookKind) []string {
	return []string{"echo " + string(kind) + " 1", "echo " + string(kind) + " 2"}
}

func hookTestConfig() WireGuardConfig {
	cfg := WireGuardConfig{PrivateKey: "server-private-key", Address: []string{"10.7.0.1/24"}, ListenPort: 51820, Hooks: apischema.WireGuardHooks{}}
	for _, kind := range apischema.WireGuardHookKinds {
		cfg.Hooks[kind] = hookTestLines(kind)
	}
	return cfg
}

// The wg-quick hook kinds are named once, in apischema.WireGuardHookKinds.
// These tests walk that list and fail if any other place handles a kind
// differently.

func TestHookKindsAreTheWGQuickKeysInFileOrder(t *testing.T) {
	want := []apischema.WireGuardHookKind{"PreUp", "PostUp", "PreDown", "PostDown"}
	if !slices.Equal(apischema.WireGuardHookKinds, want) {
		t.Fatalf("WireGuardHookKinds = %v, want %v: the file order must not change", apischema.WireGuardHookKinds, want)
	}
	union := apischema.StringEnums["WireGuardHookKind"]
	for i, kind := range apischema.WireGuardHookKinds {
		if !isInterfaceKey(string(kind)) {
			t.Fatalf("%s is not a known [Interface] key and would land in Extra", kind)
		}
		if i >= len(union) || union[i] != string(kind) {
			t.Fatalf("generated union %v does not match WireGuardHookKinds", union)
		}
	}
	// A kind is carried only through Hooks, never by a field of its own.
	for _, typ := range []reflect.Type{
		reflect.TypeFor[WireGuardConfig](),
		reflect.TypeFor[apischema.WireGuardInterface](),
		reflect.TypeFor[apischema.WireGuardUpdateInterfaceRequest](),
	} {
		for _, kind := range apischema.WireGuardHookKinds {
			if _, ok := typ.FieldByName(string(kind)); ok {
				t.Fatalf("%s has its own %s field; hooks belong in Hooks", typ.Name(), kind)
			}
		}
	}
}

func TestInterfaceHooksRoundTripWithoutLeakingIntoExtra(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wg0.conf")
	cfg := hookTestConfig()
	if err := WriteWireGuardConfig(path, cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}
	got, err := ParseWireGuardConfig(path)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if len(got.Extra) != 0 {
		t.Fatalf("hooks leaked into Extra: %v", got.Extra)
	}
	for _, kind := range apischema.WireGuardHookKinds {
		if !slices.Equal(got.Hooks[kind], cfg.Hooks[kind]) {
			t.Fatalf("%s = %v, want %v", kind, got.Hooks[kind], cfg.Hooks[kind])
		}
	}
}

func TestHooksChangedDetectsEachKind(t *testing.T) {
	base := hookTestConfig()
	if hooksChanged(base.Hooks, base.Hooks) {
		t.Fatal("identical configs reported as changed")
	}
	for _, kind := range apischema.WireGuardHookKinds {
		changed := hookTestConfig()
		changed.Hooks[kind] = append(slices.Clone(changed.Hooks[kind]), "echo extra")
		if !hooksChanged(changed.Hooks, base.Hooks) {
			t.Fatalf("a change to %s was not detected", kind)
		}
	}
}

func TestInterfaceHooksReachAPIStructs(t *testing.T) {
	newWireGuardTestEnv(t)
	cfg := hookTestConfig()
	if err := WriteWireGuardConfig(configPath("wgtest"), cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}
	interfaces, err := ListInterfaces(context.Background())
	if err != nil || len(interfaces) != 1 {
		t.Fatalf("ListInterfaces = %+v, %v", interfaces, err)
	}
	for _, kind := range apischema.WireGuardHookKinds {
		if got := interfaces[0].Hooks[kind]; !slices.Equal(got, cfg.Hooks[kind]) {
			t.Fatalf("API interface %s = %v, want %v", kind, got, cfg.Hooks[kind])
		}
	}
}

func TestInterfaceHooksApplyFromUpdateRequest(t *testing.T) {
	for _, kind := range apischema.WireGuardHookKinds {
		req := apischema.WireGuardUpdateInterfaceRequest{Hooks: apischema.WireGuardHooks{kind: {" echo new "}}}
		updated := hookTestConfig()
		if err := applyInterfaceHooks(&updated, req); err != nil {
			t.Fatalf("applyInterfaceHooks returned error: %v", err)
		}
		for _, other := range apischema.WireGuardHookKinds {
			want := hookTestLines(other)
			if other == kind {
				want = []string{"echo new"}
			}
			if !slices.Equal(updated.Hooks[other], want) {
				t.Fatalf("request %s: %s = %v, want %v", kind, other, updated.Hooks[other], want)
			}
		}
	}
}

// Decoded as the router decodes it: an absent or null kind is unchanged, an
// empty list removes the kind, and a list of blank lines removes it too.
func TestApplyInterfaceHooksKeepsPartialUpdateMeaning(t *testing.T) {
	body := `{"name":"wgtest","hooks":{"PreUp":[],"PostUp":null,"PreDown":["  ",""]}}`
	decoded, err := bridgeipc.JSONRequestDecoder[apischema.WireGuardUpdateInterfaceRequest]()(jsontext.Value(body))
	if err != nil {
		t.Fatal(err)
	}
	req, ok := decoded.(apischema.WireGuardUpdateInterfaceRequest)
	if !ok {
		t.Fatalf("decoded %T", decoded)
	}
	old := hookTestConfig()
	cfg := old
	if err := applyInterfaceHooks(&cfg, req); err != nil {
		t.Fatalf("applyInterfaceHooks returned error: %v", err)
	}
	want := map[apischema.WireGuardHookKind][]string{
		apischema.WireGuardPreUp:    nil,
		apischema.WireGuardPostUp:   hookTestLines(apischema.WireGuardPostUp),
		apischema.WireGuardPreDown:  nil,
		apischema.WireGuardPostDown: hookTestLines(apischema.WireGuardPostDown),
	}
	for kind, lines := range want {
		if !slices.Equal(cfg.Hooks[kind], lines) {
			t.Fatalf("%s = %v, want %v", kind, cfg.Hooks[kind], lines)
		}
	}
	// cfg was copied from old; the update must not reach old's hooks.
	if !slices.Equal(old.Hooks[apischema.WireGuardPreUp], hookTestLines(apischema.WireGuardPreUp)) {
		t.Fatalf("old PreUp = %v: the update leaked into the config it was copied from", old.Hooks[apischema.WireGuardPreUp])
	}
}

func TestApplyInterfaceHooksRefusesUnknownKind(t *testing.T) {
	cfg := hookTestConfig()
	req := apischema.WireGuardUpdateInterfaceRequest{Hooks: apischema.WireGuardHooks{
		apischema.WireGuardPreUp: {"echo new"},
		"preUp":                  {"echo camel"},
	}}
	err := applyInterfaceHooks(&cfg, req)
	if !errors.Is(err, bridgeipc.ErrInvalidArgs) || bridgeipc.StatusCode(err) != 400 {
		t.Fatalf("applyInterfaceHooks error = %v, want a 400", err)
	}
	if !slices.Equal(cfg.Hooks[apischema.WireGuardPreUp], hookTestLines(apischema.WireGuardPreUp)) {
		t.Fatalf("a refused request changed PreUp to %v", cfg.Hooks[apischema.WireGuardPreUp])
	}
}
