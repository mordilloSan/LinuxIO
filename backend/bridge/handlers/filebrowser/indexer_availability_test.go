package filebrowser

import (
	"context"
	"errors"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestCheckIndexerAvailabilityUsesSocketActivation(t *testing.T) {
	orig := getIndexerUnitInfo
	getIndexerUnitInfo = func(_ context.Context, unitName string) (apischema.UnitInfo, error) {
		if unitName != indexerSocketName {
			return apischema.UnitInfo{}, errors.New("unexpected unit")
		}
		return testUnitInfoState("active", "listening"), nil
	}
	t.Cleanup(func() {
		getIndexerUnitInfo = orig
		setIndexerAvailability(true)
	})

	ok, err := CheckIndexerAvailability(context.Background())
	if err != nil {
		t.Fatalf("CheckIndexerAvailability returned error: %v", err)
	}
	if !ok {
		t.Fatal("CheckIndexerAvailability returned false")
	}
	if !isIndexerEnabled() {
		t.Fatal("indexer availability cache was not set to true")
	}
}

func TestCheckIndexerAvailabilityReportsUnavailable(t *testing.T) {
	orig := getIndexerUnitInfo
	getIndexerUnitInfo = func(_ context.Context, unitName string) (apischema.UnitInfo, error) {
		if unitName != indexerSocketName {
			return apischema.UnitInfo{}, errors.New("unexpected unit")
		}
		return testUnitInfoState("inactive", "dead"), nil
	}
	t.Cleanup(func() {
		getIndexerUnitInfo = orig
		setIndexerAvailability(true)
	})

	ok, err := CheckIndexerAvailability(context.Background())
	if err == nil {
		t.Fatal("CheckIndexerAvailability returned nil error")
	}
	if ok {
		t.Fatal("CheckIndexerAvailability returned true")
	}
	if isIndexerEnabled() {
		t.Fatal("indexer availability cache was not set to false")
	}
}

func testUnitInfoState(activeState, subState string) apischema.UnitInfo {
	return apischema.UnitInfo{
		ActiveState: &activeState,
		SubState:    &subState,
	}
}
