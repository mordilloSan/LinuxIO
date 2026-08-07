package accounts

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestAccountUserDetailsToAPIKeepsKnownRootOwnership(t *testing.T) {
	result := accountUserDetailsToAPI(UserDetails{Home: UserHomeHealth{
		Exists: true, OwnerUID: 0, GroupGID: 0, ownershipKnown: true,
	}, FailedLoginAttemptsError: "journal unavailable"})
	if result.Home.OwnerUID == nil || *result.Home.OwnerUID != 0 {
		t.Fatalf("owner UID = %v, want known zero", result.Home.OwnerUID)
	}
	if result.Home.GroupGID == nil || *result.Home.GroupGID != 0 {
		t.Fatalf("group GID = %v, want known zero", result.Home.GroupGID)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"failedLoginAttemptsError":"journal unavailable"`)) {
		t.Fatalf("ordinary error should remain a scalar JSON string: %s", encoded)
	}
}

func TestAccountUserDetailsToAPIOmitsUnknownOwnership(t *testing.T) {
	result := accountUserDetailsToAPI(UserDetails{})
	if result.Home.OwnerUID != nil || result.Home.GroupGID != nil {
		t.Fatalf("unknown ownership must be omitted, got %#v", result.Home)
	}
}
