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
		switch unitName {
		case indexerSocketName:
			return testUnitInfoState("active", "listening"), nil
		case indexerServiceName:
			return testUnitInfoState("inactive", "dead"), nil
		default:
			return apischema.UnitInfo{}, errors.New("unexpected unit")
		}
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

func TestCheckIndexerAvailabilityFallsBackToRunningService(t *testing.T) {
	orig := getIndexerUnitInfo
	getIndexerUnitInfo = func(_ context.Context, unitName string) (apischema.UnitInfo, error) {
		switch unitName {
		case indexerSocketName:
			return testUnitInfoState("inactive", "dead"), nil
		case indexerServiceName:
			return testUnitInfoState("active", "running"), nil
		default:
			return apischema.UnitInfo{}, errors.New("unexpected unit")
		}
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
}

func TestCheckIndexerAvailabilityReportsUnavailable(t *testing.T) {
	orig := getIndexerUnitInfo
	getIndexerUnitInfo = func(_ context.Context, unitName string) (apischema.UnitInfo, error) {
		switch unitName {
		case indexerSocketName, indexerServiceName:
			return testUnitInfoState("inactive", "dead"), nil
		default:
			return apischema.UnitInfo{}, errors.New("unexpected unit")
		}
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
