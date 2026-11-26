package shares

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// testAccounts is alice, bob and carol, with the groups family (alice, bob
// and, as her primary group, carol) and kids (bob), beside system accounts.
func testAccounts() localAccounts {
	return localAccounts{
		users: []localUser{
			{name: "root", uid: 0, gid: 0},
			{name: "www-data", uid: 33, gid: 33},
			{name: "alice", uid: 1000, gid: 1000},
			{name: "bob", uid: 1001, gid: 1001},
			{name: "carol", uid: 1002, gid: 2000},
			{name: "nobody", uid: 65534, gid: 65534},
		},
		groups: []localGroup{
			{name: "root", gid: 0},
			{name: "docker", gid: 998, members: []string{"alice"}},
			{name: "alice", gid: 1000},
			{name: "bob", gid: 1001},
			{name: "family", gid: 2000, members: []string{"alice", "bob"}},
			{name: "kids", gid: 2001, members: []string{"bob"}},
			{name: "nogroup", gid: 65534},
		},
	}
}

func row(kind, name, level string) apischema.SharedFolderAccess {
	return apischema.SharedFolderAccess{Kind: kind, Name: name, Level: level}
}

func TestValidateAccess(t *testing.T) {
	accounts := testAccounts()
	tests := []struct {
		name     string
		access   []apischema.SharedFolderAccess
		everyone string
		ok       bool
	}{
		{"users and groups", []apischema.SharedFolderAccess{row("user", "alice", "rw"), row("group", "family", "ro")}, "none", true},
		{"no rows", nil, "rw", true},
		{"unknown level", []apischema.SharedFolderAccess{row("user", "alice", "full")}, "none", false},
		{"unknown everyone level", nil, "", false},
		{"unknown kind", []apischema.SharedFolderAccess{row("role", "alice", "rw")}, "none", false},
		{"user that does not exist", []apischema.SharedFolderAccess{row("user", "dave", "rw")}, "none", false},
		{"system user", []apischema.SharedFolderAccess{row("user", "www-data", "rw")}, "none", false},
		{"root", []apischema.SharedFolderAccess{row("user", "root", "rw")}, "none", false},
		{"nobody", []apischema.SharedFolderAccess{row("user", "nobody", "rw")}, "none", false},
		{"system group", []apischema.SharedFolderAccess{row("group", "docker", "rw")}, "none", false},
		{"a group named as a user", []apischema.SharedFolderAccess{row("user", "family", "rw")}, "none", false},
		{"the same row twice", []apischema.SharedFolderAccess{row("user", "alice", "rw"), row("user", "alice", "ro")}, "none", false},
		{"a user and a group of one name", []apischema.SharedFolderAccess{row("user", "alice", "rw"), row("group", "alice", "ro")}, "none", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAccess(tt.access, tt.everyone, accounts)
			if tt.ok && err != nil {
				t.Fatalf("validateAccess() = %v, want nil", err)
			}
			if !tt.ok && !errors.Is(err, bridgeipc.ErrInvalidArgs) {
				t.Fatalf("validateAccess() = %v, want ErrInvalidArgs", err)
			}
		})
	}
}

// No access wins over everything, then read and write over read only, among
// a person's own row and the rows of their groups; a person no row matches
// is left to everyone else.
func TestPlanAccess(t *testing.T) {
	accounts := testAccounts()
	tests := []struct {
		name       string
		access     []apischema.SharedFolderAccess
		wantUsers  map[int]string
		wantGroups map[int]string
	}{
		{
			name:       "a user's own row",
			access:     []apischema.SharedFolderAccess{row("user", "alice", "ro")},
			wantUsers:  map[int]string{1000: "ro"},
			wantGroups: map[int]string{},
		},
		{
			name:       "a group grants its members by its own entry",
			access:     []apischema.SharedFolderAccess{row("group", "family", "rw")},
			wantUsers:  map[int]string{},
			wantGroups: map[int]string{2000: "rw"},
		},
		{
			name:       "read and write from a group wins over a user's read only",
			access:     []apischema.SharedFolderAccess{row("user", "alice", "ro"), row("group", "family", "rw")},
			wantUsers:  map[int]string{1000: "rw"},
			wantGroups: map[int]string{2000: "rw"},
		},
		{
			name:       "a user's read and write stays over a group's read only",
			access:     []apischema.SharedFolderAccess{row("user", "alice", "rw"), row("group", "family", "ro")},
			wantUsers:  map[int]string{1000: "rw"},
			wantGroups: map[int]string{2000: "ro"},
		},
		{
			name:   "no access on a group reaches each member, a granted one too",
			access: []apischema.SharedFolderAccess{row("user", "bob", "rw"), row("group", "family", "rw"), row("group", "kids", "none")},
			// bob is in kids: no access, over his own row and over family.
			wantUsers:  map[int]string{1001: "none"},
			wantGroups: map[int]string{2000: "rw", 2001: "none"},
		},
		{
			name:   "a primary group counts as membership",
			access: []apischema.SharedFolderAccess{row("user", "carol", "ro"), row("group", "family", "none")},
			// carol's primary group is family; alice and bob are listed in it.
			wantUsers:  map[int]string{1000: "none", 1001: "none", 1002: "none"},
			wantGroups: map[int]string{2000: "none"},
		},
		{
			name:       "a user's own no access",
			access:     []apischema.SharedFolderAccess{row("user", "alice", "none"), row("group", "family", "rw")},
			wantUsers:  map[int]string{1000: "none"},
			wantGroups: map[int]string{2000: "rw"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := planAccess(tt.access, "ro", accounts)
			if !maps.Equal(plan.users, tt.wantUsers) {
				t.Errorf("users = %v, want %v", plan.users, tt.wantUsers)
			}
			if !maps.Equal(plan.groups, tt.wantGroups) {
				t.Errorf("groups = %v, want %v", plan.groups, tt.wantGroups)
			}
			if plan.everyone != "ro" {
				t.Errorf("everyone = %q, want ro", plan.everyone)
			}
		})
	}
}

func TestSambaAccessProperties(t *testing.T) {
	access := []apischema.SharedFolderAccess{
		row("user", "alice", "rw"),
		row("user", "bob", "ro"),
		row("group", "family", "ro"),
		row("group", "kids", "none"),
	}
	tests := []struct {
		name     string
		access   []apischema.SharedFolderAccess
		everyone string
		want     map[string]string
	}{
		{
			name:     "everyone else has no access: only the rows may connect",
			access:   access,
			everyone: "none",
			want: map[string]string{
				"valid users":   "alice, bob, +family",
				"invalid users": "+kids",
				"read only":     "yes",
				"write list":    "alice",
			},
		},
		{
			name:     "everyone else reads: anyone connects, the rows with write may write",
			access:   access,
			everyone: "ro",
			want: map[string]string{
				"invalid users": "+kids",
				"read only":     "yes",
				"write list":    "alice",
			},
		},
		{
			name:     "everyone else writes: the read-only rows are held to reading",
			access:   access,
			everyone: "rw",
			want: map[string]string{
				"invalid users": "+kids",
				"read only":     "no",
				"read list":     "bob, +family",
				"write list":    "alice",
			},
		},
		{
			name:     "no rows",
			everyone: "rw",
			want:     map[string]string{"read only": "no"},
		},
		{
			// An empty "valid users" would let everyone in.
			name:     "nobody is let in: the share is closed",
			access:   []apischema.SharedFolderAccess{row("group", "kids", "none")},
			everyone: "none",
			want:     map[string]string{"read only": "yes", "invalid users": "+kids", "available": "no"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sambaAccessProperties(tt.access, tt.everyone)
			if !maps.Equal(got, tt.want) {
				t.Errorf("sambaAccessProperties() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGrantsNobody(t *testing.T) {
	if !grantsNobody(nil, "none") {
		t.Error("no rows and no access for everyone else grants somebody")
	}
	if !grantsNobody([]apischema.SharedFolderAccess{row("user", "alice", "none")}, "none") {
		t.Error("only a no-access row grants somebody")
	}
	if grantsNobody([]apischema.SharedFolderAccess{row("group", "family", "ro")}, "none") {
		t.Error("a read-only group grants nobody")
	}
	if grantsNobody(nil, "ro") {
		t.Error("read only for everyone else grants nobody")
	}
}

func TestReadLocalAccounts(t *testing.T) {
	dir := t.TempDir()
	passwd := filepath.Join(dir, "passwd")
	group := filepath.Join(dir, "group")
	if err := os.WriteFile(passwd, []byte("root:x:0:0:root:/root:/bin/bash\n# a comment\n\nalice:x:1000:1000:Alice,,,:/home/alice:/bin/bash\nbroken:x:notanumber:1:::\n+::::::\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(group, []byte("root:x:0:\nfamily:x:2000:alice,bob\nempty:x:2001:\nbad:x:nan:alice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	origPasswd, origGroup := passwdPath, groupPath
	passwdPath, groupPath = passwd, group
	t.Cleanup(func() { passwdPath, groupPath = origPasswd, origGroup })

	accounts, err := readLocalAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts.users) != 2 || accounts.users[1] != (localUser{name: "alice", uid: 1000, gid: 1000}) {
		t.Errorf("users = %+v", accounts.users)
	}
	if len(accounts.groups) != 3 {
		t.Fatalf("groups = %+v", accounts.groups)
	}
	family := accounts.groups[1]
	if family.name != "family" || family.gid != 2000 || len(family.members) != 2 || family.members[1] != "bob" {
		t.Errorf("family = %+v", family)
	}
	if len(accounts.groups[2].members) != 0 {
		t.Errorf("empty group has members %v", accounts.groups[2].members)
	}
}

// sambaLevel is what smbd gives user, a member of groups, under the lines of
// a share's section: "available" and "invalid users" refuse first, a
// non-empty "valid users" keeps out whoever it does not name, then "read
// only" is the start, a name in "read list" makes it read only and one in
// "write list" writable (is_share_read_only_for_token).
func sambaLevel(props map[string]string, user string, groups []string) string {
	names := func(key string) []string {
		var out []string
		for name := range strings.SplitSeq(props[key], ",") {
			if name = strings.TrimSpace(name); name != "" {
				out = append(out, name)
			}
		}
		return out
	}
	in := func(key string) bool {
		return slices.ContainsFunc(names(key), func(name string) bool {
			group, isGroup := strings.CutPrefix(name, "+")
			if isGroup {
				return slices.Contains(groups, group)
			}
			return name == user
		})
	}
	if props["available"] == "no" || in("invalid users") {
		return levelNone
	}
	if len(names("valid users")) > 0 && !in("valid users") {
		return levelNone
	}
	readOnly := props["read only"] != "no"
	if in("read list") {
		readOnly = true
	}
	if in("write list") {
		readOnly = false
	}
	if readOnly {
		return levelRO
	}
	return levelRW
}

// aclLevel is what the kernel gives uid, a member of gids, on a folder root
// owns with the ACL acl (posix_acl_permission): a named user entry decides
// alone; else the group entries that match grant what any of them grants,
// and refuse when none does; else everyone else's entry.
func aclLevel(acl btrfs.FolderACL, uid int, gids []int) string {
	level := func(perm btrfs.ACLPerm) string {
		switch perm {
		case btrfs.ACLReadWrite:
			return levelRW
		case btrfs.ACLRead:
			return levelRO
		case btrfs.ACLNone:
		}
		return levelNone
	}
	if perm, ok := acl.Users[uid]; ok {
		return level(perm)
	}
	matched := false
	best := btrfs.ACLNone
	for _, gid := range gids {
		if perm, ok := acl.Groups[gid]; ok {
			matched = true
			if perm > best {
				best = perm
			}
		}
	}
	if matched {
		return level(best)
	}
	return level(acl.Other)
}

// The rule, written once more by hand: no access wins, then read and write,
// then read only, among the person's own row and their groups' rows; else
// everyone else's level.
func ruleLevel(own string, groupRows []string, everyone string) string {
	levels := append([]string{own}, groupRows...)
	for _, level := range []string{levelNone, levelRW, levelRO} {
		if slices.Contains(levels, level) {
			return level
		}
	}
	return everyone
}

// accessCase is one person's place in a table: their own row, the rows of
// two groups and whether they are in each, and everyone else's level. An
// empty row is none.
type accessCase struct {
	own, g1, g2 string
	inG1, inG2  bool
	everyone    string
}

// checkAccessCase checks that Samba's lists and the folder's ACL, worked out
// from the table of c, give alice what the rule says.
func checkAccessCase(t *testing.T, c accessCase) {
	t.Helper()
	accounts := localAccounts{
		users: []localUser{{name: "alice", uid: 1000, gid: 1000}, {name: "bob", uid: 1001, gid: 1001}},
		groups: []localGroup{
			{name: "alice", gid: 1000}, {name: "bob", gid: 1001},
			{name: "g1", gid: 2000}, {name: "g2", gid: 2001},
		},
	}
	groups, gids, groupRows := []string{"alice"}, []int{1000}, []string{}
	for i, in := range []bool{c.inG1, c.inG2} {
		if in {
			g := &accounts.groups[2+i]
			g.members = []string{"alice"}
			groups, gids, groupRows = append(groups, g.name), append(gids, g.gid), append(groupRows, []string{c.g1, c.g2}[i])
		}
	}
	// bob has a row of his own, so the table always names someone.
	access := []apischema.SharedFolderAccess{row("user", "bob", levelRO)}
	for _, r := range []apischema.SharedFolderAccess{row("user", "alice", c.own), row("group", "g1", c.g1), row("group", "g2", c.g2)} {
		if r.Level != "" {
			access = append(access, r)
		}
	}
	if err := validateAccess(access, c.everyone, accounts); err != nil {
		t.Fatal(err)
	}
	want := ruleLevel(c.own, groupRows, c.everyone)
	folder := apischema.SharedFolder{Access: access, Everyone: c.everyone}
	smb := sambaLevel(sambaAccessProperties(access, c.everyone), "alice", groups)
	kernel := aclLevel(folderACL(folder, accounts), 1000, gids)
	if smb != want || kernel != want {
		t.Errorf("%+v: rule %s, Samba %s, ACL %s", c, want, smb, kernel)
	}
}

// Samba's lists and the folder's ACL, each worked out from the same table,
// give every person what the rule says, for every combination of their own
// row, the rows of two groups they may be in, and everyone else.
func TestSambaAndTheACLGiveWhatTheRuleSays(t *testing.T) {
	rows := []string{"", levelNone, levelRO, levelRW}
	everyones := []string{levelNone, levelRO, levelRW}
	// One number counts through every combination, a digit each.
	total := len(rows) * len(rows) * len(rows) * 4 * len(everyones)
	for n := range total {
		d := n
		next := func(base int) int { v := d % base; d /= base; return v }
		c := accessCase{own: rows[next(4)], g1: rows[next(4)], g2: rows[next(4)]}
		membership := next(4)
		c.inG1, c.inG2 = membership&1 != 0, membership&2 != 0
		c.everyone = everyones[next(3)]
		checkAccessCase(t, c)
	}
	if total != 768 {
		t.Fatalf("checked %d combinations", total)
	}
}
