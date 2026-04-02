package accounts

import "testing"

func TestNormalizeGroupMembers(t *testing.T) {
	tests := []struct {
		name    string
		members []string
		want    []string
		wantErr bool
	}{
		{
			name:    "trims and deduplicates while preserving order",
			members: []string{" alice ", "bob", "alice", "carol"},
			want:    []string{"alice", "bob", "carol"},
		},
		{
			name:    "empty usernames are rejected",
			members: []string{"alice", "   "},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeGroupMembers(tc.members)
			if (err != nil) != tc.wantErr {
				t.Fatalf("normalizeGroupMembers() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len(normalizeGroupMembers()) = %d, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("normalizeGroupMembers()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestSameGroupMembers(t *testing.T) {
	tests := []struct {
		name    string
		current []string
		desired []string
		want    bool
	}{
		{
			name:    "matches same members in different order",
			current: []string{"alice", "bob"},
			desired: []string{"bob", "alice"},
			want:    true,
		},
		{
			name:    "ignores blank current entries",
			current: []string{"alice", "", "bob"},
			desired: []string{"bob", "alice"},
			want:    true,
		},
		{
			name:    "detects missing member",
			current: []string{"alice"},
			desired: []string{"alice", "bob"},
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameGroupMembers(tc.current, tc.desired); got != tc.want {
				t.Fatalf("sameGroupMembers() = %v, want %v", got, tc.want)
			}
		})
	}
}
