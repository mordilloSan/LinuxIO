package power

import "testing"

func TestBoolStringResultSupportsFlatBody(t *testing.T) {
	ok, msg, err := boolStringResult([]any{true, "OK"})
	if err != nil {
		t.Fatalf("boolStringResult error: %v", err)
	}
	if !ok || msg != "OK" {
		t.Fatalf("got ok=%v msg=%q", ok, msg)
	}
}

func TestBoolStringResultSupportsStructBody(t *testing.T) {
	ok, msg, err := boolStringResult([]any{[]any{false, "bad profile"}})
	if err != nil {
		t.Fatalf("boolStringResult error: %v", err)
	}
	if ok || msg != "bad profile" {
		t.Fatalf("got ok=%v msg=%q", ok, msg)
	}
}

func TestBoolStringResultSupportsReflectedStructBody(t *testing.T) {
	ok, msg, err := boolStringResult([]any{struct {
		OK      bool
		Message string
	}{true, "OK"}})
	if err != nil {
		t.Fatalf("boolStringResult error: %v", err)
	}
	if !ok || msg != "OK" {
		t.Fatalf("got ok=%v msg=%q", ok, msg)
	}
}

func TestBoolStringResultSupportsAutoProfileBody(t *testing.T) {
	ok, msg, err := boolStringResult([]any{true, "balanced"})
	if err != nil {
		t.Fatalf("boolStringResult error: %v", err)
	}
	if !ok || msg != "balanced" {
		t.Fatalf("got ok=%v msg=%q", ok, msg)
	}
}

func TestProfilesFromRecords(t *testing.T) {
	profiles := profilesFromRecords([]tunedProfileRecord{
		{Name: "balanced", Description: "General profile"},
		{Name: "powersave", Description: "Power saving"},
	})
	markProfiles(profiles, "balanced", "powersave")

	if len(profiles) != 2 {
		t.Fatalf("profile count=%d", len(profiles))
	}
	if !profiles[0].Active {
		t.Fatalf("balanced should be active: %+v", profiles[0])
	}
	if !profiles[1].Recommended {
		t.Fatalf("powersave should be recommended: %+v", profiles[1])
	}
}

func TestParseOSRelease(t *testing.T) {
	values := parseOSRelease([]byte(`ID=rocky
ID_LIKE="rhel centos fedora"
NAME="Rocky Linux"`))

	if values["ID"] != "rocky" {
		t.Fatalf("ID=%q", values["ID"])
	}
	if values["ID_LIKE"] != "rhel centos fedora" {
		t.Fatalf("ID_LIKE=%q", values["ID_LIKE"])
	}
}

func TestUnitFileStateAvailability(t *testing.T) {
	for _, state := range []string{"enabled", "disabled", "static", "linked"} {
		if !isKnownUnitFileState(state) {
			t.Fatalf("%q should be a known unit file state", state)
		}
		if !isStartableUnitFileState(state) {
			t.Fatalf("%q should be startable", state)
		}
	}

	for _, state := range []string{"", "bad", "not-found", "masked", "masked-runtime"} {
		if isStartableUnitFileState(state) {
			t.Fatalf("%q should not be startable", state)
		}
	}
}
