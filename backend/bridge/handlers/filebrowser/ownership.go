package filebrowser

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func resolveUserID(identifier string) (int, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return -1, nil
	}

	if u, err := user.Lookup(trimmed); err == nil {
		return strconv.Atoi(u.Uid)
	}

	if u, err := user.LookupId(trimmed); err == nil {
		return strconv.Atoi(u.Uid)
	}

	if id, err := strconv.Atoi(trimmed); err == nil {
		return id, nil
	}

	return -1, fmt.Errorf("unknown user: %s", trimmed)
}

func resolveGroupID(identifier string) (int, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return -1, nil
	}

	if g, err := user.LookupGroup(trimmed); err == nil {
		return strconv.Atoi(g.Gid)
	}

	if g, err := user.LookupGroupId(trimmed); err == nil {
		return strconv.Atoi(g.Gid)
	}

	if id, err := strconv.Atoi(trimmed); err == nil {
		return id, nil
	}

	return -1, fmt.Errorf("unknown group: %s", trimmed)
}

// usersGroups returns lists of all users and groups on the system
// Args: []
func usersGroups(ctx context.Context) (apischema.UsersGroupsResponse, error) {
	users, err := getAllUsers(ctx)
	if err != nil {
		slog.Debug("error getting users", "error", err)
		return apischema.UsersGroupsResponse{}, fmt.Errorf("error getting users: %w", err)
	}

	groups, err := getAllGroups(ctx)
	if err != nil {
		slog.Debug("error getting groups", "error", err)
		return apischema.UsersGroupsResponse{}, fmt.Errorf("error getting groups: %w", err)
	}

	return apischema.UsersGroupsResponse{Users: users, Groups: groups}, nil
}

func getAllUsers(ctx context.Context) ([]string, error) {
	content, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return nil, err
	}

	users := []string{}
	lines := strings.SplitSeq(string(content), "\n")
	for line := range lines {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: username:password:UID:GID:GECOS:home:shell
		parts := strings.Split(line, ":")
		if len(parts) > 0 {
			username := strings.TrimSpace(parts[0])
			if username != "" {
				users = append(users, username)
			}
		}
	}

	return users, nil
}

func getAllGroups(ctx context.Context) ([]string, error) {
	content, err := os.ReadFile("/etc/group")
	if err != nil {
		return nil, err
	}

	groups := []string{}
	lines := strings.SplitSeq(string(content), "\n")
	for line := range lines {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: groupname:password:GID:user_list
		parts := strings.Split(line, ":")
		if len(parts) > 0 {
			groupname := strings.TrimSpace(parts[0])
			if groupname != "" {
				groups = append(groups, groupname)
			}
		}
	}

	return groups, nil
}
