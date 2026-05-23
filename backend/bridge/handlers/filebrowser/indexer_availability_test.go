package filebrowser

import (
	"context"
	"errors"
	"testing"
)

func TestCheckIndexerAvailabilityUsesSocketActivation(t *testing.T) {
	orig := getIndexerUnitInfo
	getIndexerUnitInfo = func(_ context.Context, unitName string) (map[string]any, error) {
		switch unitName {
		case indexerSocketName:
			return map[string]any{
				"ActiveState": "active",
				"SubState":    "listening",
			}, nil
		case indexerServiceName:
			return map[string]any{
				"ActiveState": "inactive",
				"SubState":    "dead",
			}, nil
		default:
			return nil, errors.New("unexpected unit")
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
	getIndexerUnitInfo = func(_ context.Context, unitName string) (map[string]any, error) {
		switch unitName {
		case indexerSocketName:
			return map[string]any{
				"ActiveState": "inactive",
				"SubState":    "dead",
			}, nil
		case indexerServiceName:
			return map[string]any{
				"ActiveState": "active",
				"SubState":    "running",
			}, nil
		default:
			return nil, errors.New("unexpected unit")
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
	getIndexerUnitInfo = func(_ context.Context, unitName string) (map[string]any, error) {
		switch unitName {
		case indexerSocketName, indexerServiceName:
			return map[string]any{
				"ActiveState": "inactive",
				"SubState":    "dead",
			}, nil
		default:
			return nil, errors.New("unexpected unit")
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
