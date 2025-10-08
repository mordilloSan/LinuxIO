package session

import (
	"testing"
	"time"
)

func TestMemStore_CommitFindDelete(t *testing.T) {
	st := NewWithCleanupInterval(0)

	tok := "abc"
	data := []byte("hello")
	exp := time.Now().Add(100 * time.Millisecond)

	if err := st.Commit(tok, data, exp); err != nil {
		t.Fatalf("Commit error: %v", err)
	}

	got, ok, err := st.Find(tok)
	if err != nil || !ok {
		t.Fatalf("Find failed ok=%v err=%v", ok, err)
	}
	if string(got) != string(data) {
		t.Fatalf("Find returned %q, want %q", string(got), string(data))
	}

	if err := st.Delete(tok); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if _, ok, _ := st.Find(tok); ok {
		t.Fatalf("Find should report not found after delete")
	}
}

func TestMemStore_AllFiltersExpired(t *testing.T) {
	st := NewWithCleanupInterval(0)
	now := time.Now()

	// Active
	_ = st.Commit("live", []byte("x"), now.Add(50*time.Millisecond))
	// Expired
	_ = st.Commit("dead", []byte("y"), now.Add(-10*time.Millisecond))

	mm, err := st.All()
	if err != nil {
		t.Fatalf("All error: %v", err)
	}
	if _, ok := mm["dead"]; ok {
		t.Fatalf("All should not include expired entries")
	}
	if _, ok := mm["live"]; !ok {
		t.Fatalf("All should include live entries")
	}
}
