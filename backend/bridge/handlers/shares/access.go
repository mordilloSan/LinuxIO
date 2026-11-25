package shares

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// The access table of a shared folder (docs/shared-folders.md, "Access"):
// each row gives a local user or group read and write, read only or no
// access, and one more level covers everyone else. No access wins over
// everything; otherwise the most access among a person's own row and the
// rows of their groups applies; everyone else is whoever matches no row.
//
// The same rule is written twice: as the lists of the share's smb.conf
// section (sambaAccessProperties), which Samba reads at each connection, and
// as the folder's POSIX ACL (planAccess, acl.go), which the kernel reads for
// local and NFS access and for what Samba does as the person signed in.

const (
	levelRW   = "rw"
	levelRO   = "ro"
	levelNone = "none"

	kindUser  = "user"
	kindGroup = "group"

	// firstRegularID is where regular users and groups start, as the Accounts
	// page tells them from system ones; nobodyID is the overflow account.
	firstRegularID = 1000
	nobodyID       = 65534
)

// passwdPath and groupPath are vars so tests can point them at their own
// files.
var (
	passwdPath = "/etc/passwd"
	groupPath  = "/etc/group"
)

// validAccountName is a name that can stand in a Samba list, where a space
// or a comma separates two names.
var validAccountName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*\$?$`)

type localUser struct {
	name     string
	uid, gid int
}

type localGroup struct {
	name    string
	gid     int
	members []string
}

// localAccounts is the users and groups of /etc/passwd and /etc/group, the
// only ones an access table may name.
type localAccounts struct {
	users  []localUser
	groups []localGroup
}

func (a localAccounts) user(name string) (localUser, bool) {
	i := slices.IndexFunc(a.users, func(u localUser) bool { return u.name == name })
	if i < 0 {
		return localUser{}, false
	}
	return a.users[i], true
}

func (a localAccounts) group(name string) (localGroup, bool) {
	i := slices.IndexFunc(a.groups, func(g localGroup) bool { return g.name == name })
	if i < 0 {
		return localGroup{}, false
	}
	return a.groups[i], true
}

// membersOf returns the users in g: those its line lists and those whose
// primary group it is.
func (a localAccounts) membersOf(g localGroup) []localUser {
	var members []localUser
	for _, u := range a.users {
		if u.gid == g.gid || slices.Contains(g.members, u.name) {
			members = append(members, u)
		}
	}
	return members
}

func regularID(id int) bool { return id >= firstRegularID && id != nobodyID }

// readLocalAccounts reads /etc/passwd and /etc/group. A line it cannot read
// (a comment, an NIS entry, a broken one) is skipped.
func readLocalAccounts() (localAccounts, error) {
	var accounts localAccounts
	err := eachColonLine(passwdPath, func(fields []string) {
		if len(fields) < 4 {
			return
		}
		uid, uidErr := strconv.Atoi(fields[2])
		gid, gidErr := strconv.Atoi(fields[3])
		if uidErr != nil || gidErr != nil {
			return
		}
		accounts.users = append(accounts.users, localUser{name: fields[0], uid: uid, gid: gid})
	})
	if err != nil {
		return localAccounts{}, err
	}
	err = eachColonLine(groupPath, func(fields []string) {
		if len(fields) < 4 {
			return
		}
		gid, gidErr := strconv.Atoi(fields[2])
		if gidErr != nil {
			return
		}
		group := localGroup{name: fields[0], gid: gid}
		for member := range strings.SplitSeq(fields[3], ",") {
			if member = strings.TrimSpace(member); member != "" {
				group.members = append(group.members, member)
			}
		}
		accounts.groups = append(accounts.groups, group)
	})
	if err != nil {
		return localAccounts{}, err
	}
	return accounts, nil
}

func eachColonLine(path string, fn func(fields []string)) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fn(strings.Split(line, ":"))
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

func validLevel(level string) bool {
	return level == levelRW || level == levelRO || level == levelNone
}

// rowID returns the UID or GID of the account a row names now, and false
// when there is none or it is another account than the one the row was
// given to (a different ID: the account was deleted and made again).
func rowID(row apischema.SharedFolderAccess, accounts localAccounts) (int, bool) {
	id, found := -1, false
	switch row.Kind {
	case kindUser:
		var user localUser
		user, found = accounts.user(row.Name)
		id = user.uid
	case kindGroup:
		var group localGroup
		group, found = accounts.group(row.Name)
		id = group.gid
	}
	return id, found && (row.ID == 0 || row.ID == id)
}

// withAccountIDs returns access with each row's ID set to the account it
// names now; the table has been validated.
func withAccountIDs(access []apischema.SharedFolderAccess, accounts localAccounts) []apischema.SharedFolderAccess {
	out := slices.Clone(access)
	for i := range out {
		out[i].ID, _ = rowID(out[i], accounts)
	}
	return out
}

// validateAccess checks a table against the local accounts: each row names a
// regular user or group once, with a level, and still the account it was
// given to, and everyone else has a level too.
func validateAccess(access []apischema.SharedFolderAccess, everyone string, accounts localAccounts) error {
	if !validLevel(everyone) {
		return fmt.Errorf("%w: access level %q for everyone else", bridgeipc.ErrInvalidArgs, everyone)
	}
	seen := make(map[apischema.SharedFolderAccess]bool, len(access))
	for _, row := range access {
		if !validLevel(row.Level) {
			return fmt.Errorf("%w: access level %q for %s", bridgeipc.ErrInvalidArgs, row.Level, row.Name)
		}
		if !validAccountName.MatchString(row.Name) {
			return fmt.Errorf("%w: account name %q", bridgeipc.ErrInvalidArgs, row.Name)
		}
		var id int
		switch row.Kind {
		case kindUser:
			user, ok := accounts.user(row.Name)
			if !ok {
				return fmt.Errorf("%w: no local user %s", bridgeipc.ErrInvalidArgs, row.Name)
			}
			id = user.uid
		case kindGroup:
			group, ok := accounts.group(row.Name)
			if !ok {
				return fmt.Errorf("%w: no local group %s", bridgeipc.ErrInvalidArgs, row.Name)
			}
			id = group.gid
		default:
			return fmt.Errorf("%w: access row kind %q", bridgeipc.ErrInvalidArgs, row.Kind)
		}
		if !regularID(id) {
			return fmt.Errorf("%w: %s %s is a system account", bridgeipc.ErrInvalidArgs, row.Kind, row.Name)
		}
		if _, same := rowID(row, accounts); !same {
			return fmt.Errorf("%w: %s %s is another account than the one this row was given to; remove the row and add it again", bridgeipc.ErrInvalidArgs, row.Kind, row.Name)
		}
		key := apischema.SharedFolderAccess{Kind: row.Kind, Name: row.Name}
		if seen[key] {
			return fmt.Errorf("%w: %s %s has two rows", bridgeipc.ErrInvalidArgs, row.Kind, row.Name)
		}
		seen[key] = true
	}
	return nil
}

// accessPlan is a table worked out for the folder's ACL: the level of each
// user and group entry, by ID, and of everyone else.
type accessPlan struct {
	users    map[int]string
	groups   map[int]string
	everyone string
}

// strongest returns the level that applies when both do: no access first,
// then read and write, then read only. An empty level is no row.
func strongest(a, b string) string {
	for _, level := range []string{levelNone, levelRW, levelRO} {
		if a == level || b == level {
			return level
		}
	}
	return ""
}

// planAccess works a validated table out for the ACL. The kernel lets a
// user's own entry decide alone and otherwise grants what any of their
// groups grants, so the table's rule is written into the user entries: a
// user with a row gets the strongest level of that row and of their groups'
// rows, and each member of a group with no access gets an entry of their
// own saying so, which no other group can override. A row whose account no
// longer exists is left out.
//
// A member who joins a no-access group later, and is also in a group with
// access, has no entry until the access is applied again (ReapplyAccess);
// Samba, which reads the groups at each connection, refuses them at once.
func planAccess(access []apischema.SharedFolderAccess, everyone string, accounts localAccounts) accessPlan {
	plan := accessPlan{users: map[int]string{}, groups: map[int]string{}, everyone: everyone}
	// byGroup holds, per user ID, the strongest level of the rows of the
	// groups the user is in.
	byGroup := map[int]string{}
	for _, row := range access {
		if row.Kind != kindGroup {
			continue
		}
		group, ok := accounts.group(row.Name)
		if _, same := rowID(row, accounts); !ok || !same {
			continue
		}
		plan.groups[group.gid] = row.Level
		for _, member := range accounts.membersOf(group) {
			byGroup[member.uid] = strongest(byGroup[member.uid], row.Level)
		}
	}
	for _, row := range access {
		if row.Kind != kindUser {
			continue
		}
		if user, ok := accounts.user(row.Name); ok {
			if _, same := rowID(row, accounts); same {
				plan.users[user.uid] = strongest(row.Level, byGroup[user.uid])
			}
		}
	}
	for uid, level := range byGroup {
		if level == levelNone {
			plan.users[uid] = levelNone
		}
	}
	return plan
}

// sambaName is how a row is written in a Samba list: a group with "+", which
// looks it up in the system's groups only.
func sambaName(row apischema.SharedFolderAccess) string {
	if row.Kind == kindGroup {
		return "+" + row.Name
	}
	return row.Name
}

// sambaAccessProperties returns the lines of a share's section that hold its
// access table. "invalid users" is checked first, so no access wins; a name
// in both "read list" and "write list" may write, so read and write wins
// over read only; "read only" is what everyone else gets, and "valid users"
// keeps them out when that is no access. A table that lets nobody in is
// written as a share that is not available.
func sambaAccessProperties(access []apischema.SharedFolderAccess, everyone string) map[string]string {
	lists := map[string][]string{}
	for _, row := range access {
		lists[row.Level] = append(lists[row.Level], sambaName(row))
	}
	props := map[string]string{"read only": "yes"}
	set := func(key string, names ...[]string) {
		if all := slices.Concat(names...); len(all) > 0 {
			props[key] = strings.Join(all, ", ")
		}
	}
	set("invalid users", lists[levelNone])
	set("write list", lists[levelRW])
	switch everyone {
	case levelRW:
		props["read only"] = "no"
		set("read list", lists[levelRO])
	case levelNone:
		set("valid users", lists[levelRW], lists[levelRO])
	}
	// Samba reads an empty "valid users" as everyone, so a table that lets
	// nobody in closes the share instead.
	if grantsNobody(access, everyone) {
		props["available"] = "no"
	}
	return props
}

// grantsNobody says a table lets no one in: nobody has a row with access and
// everyone else has none.
func grantsNobody(access []apischema.SharedFolderAccess, everyone string) bool {
	if everyone != levelNone {
		return false
	}
	return !slices.ContainsFunc(access, func(row apischema.SharedFolderAccess) bool {
		return row.Level != levelNone
	})
}
