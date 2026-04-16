package accounts

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	groupFile = "/etc/group"
	systemGID = 1000
)

// ListGroups returns all system groups
func ListGroups() ([]Group, error) {
	groups := []Group{}

	file, err := os.Open(groupFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open group file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) < 4 {
			continue
		}

		gid, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}

		members := []string{}
		if parts[3] != "" {
			for member := range strings.SplitSeq(parts[3], ",") {
				member = strings.TrimSpace(member)
				if member != "" {
					members = append(members, member)
				}
			}
		}

		group := Group{
			Name:     parts[0],
			GID:      gid,
			Members:  members,
			IsSystem: gid < systemGID,
		}

		groups = append(groups, group)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading group file: %w", err)
	}

	return groups, nil
}

// GetGroup returns a single group by name
func GetGroup(name string) (*Group, error) {
	groups, err := ListGroups()
	if err != nil {
		return nil, err
	}

	for _, group := range groups {
		if group.Name == name {
			return &group, nil
		}
	}

	return nil, fmt.Errorf("group not found: %s", name)
}

// CreateGroup creates a new system group
func CreateGroup(req CreateGroupRequest) error {
	if req.Name == "" {
		return fmt.Errorf("group name is required")
	}

	args := []string{}

	if req.GID != nil {
		args = append(args, "-g", strconv.Itoa(*req.GID))
	}

	args = append(args, req.Name)

	cmd := exec.Command("groupadd", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create group: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("group created", "group", req.Name)
	return nil
}

// DeleteGroup deletes a system group
func DeleteGroup(name string) error {
	if name == "" {
		return fmt.Errorf("group name is required")
	}

	if name == "root" {
		return fmt.Errorf("cannot delete root group")
	}

	// Check if group exists
	_, err := GetGroup(name)
	if err != nil {
		return err
	}

	// Check if group is a primary group for any user
	users, err := ListUsers()
	if err != nil {
		return fmt.Errorf("failed to check users: %w", err)
	}

	group, _ := GetGroup(name)
	for _, user := range users {
		if user.GID == group.GID {
			return fmt.Errorf("cannot delete group: it is the primary group of user '%s'", user.Username)
		}
	}

	cmd := exec.Command("groupdel", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete group: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("group deleted", "group", name)
	return nil
}

func normalizeGroupMembers(members []string) ([]string, error) {
	normalized := make([]string, 0, len(members))
	seen := make(map[string]struct{}, len(members))

	for _, member := range members {
		member = strings.TrimSpace(member)
		if member == "" {
			return nil, fmt.Errorf("group members cannot contain empty usernames")
		}
		if _, ok := seen[member]; ok {
			continue
		}
		seen[member] = struct{}{}
		normalized = append(normalized, member)
	}

	return normalized, nil
}

func sameGroupMembers(current, desired []string) bool {
	currentSet := make(map[string]struct{}, len(current))

	for _, member := range current {
		member = strings.TrimSpace(member)
		if member == "" {
			continue
		}
		currentSet[member] = struct{}{}
	}

	if len(currentSet) != len(desired) {
		return false
	}

	for _, member := range desired {
		if _, ok := currentSet[member]; !ok {
			return false
		}
	}

	return true
}

// ModifyGroupMembers sets the members of a group
func ModifyGroupMembers(req ModifyGroupMembersRequest) error {
	if req.GroupName == "" {
		return fmt.Errorf("group name is required")
	}

	if req.GroupName == "root" {
		return fmt.Errorf("cannot modify root group")
	}

	// Check if group exists
	group, err := GetGroup(req.GroupName)
	if err != nil {
		return err
	}

	members, err := normalizeGroupMembers(req.Members)
	if err != nil {
		return err
	}

	// Validate all users exist
	for _, member := range members {
		if _, userErr := GetUser(member); userErr != nil {
			return fmt.Errorf("user not found: %s", member)
		}
	}

	if sameGroupMembers(group.Members, members) {
		slog.Info("group members unchanged", "group", req.GroupName)
		return nil
	}

	cmd := exec.Command("gpasswd", "-M", strings.Join(members, ","), req.GroupName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set group members for %s: %s", req.GroupName, strings.TrimSpace(string(output)))
	}
	slog.Info("group members modified", "group", req.GroupName)
	return nil
}
