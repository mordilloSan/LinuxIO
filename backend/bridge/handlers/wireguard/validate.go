package wireguard

import (
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"
)

// validInterfaceName matches valid WireGuard interface names (alphanumeric, underscore, hyphen)
var validInterfaceName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// validateInterfaceName checks if the interface name is safe to use in file paths
func validateInterfaceName(name string) error {
	if name == "" {
		return fmt.Errorf("interface name cannot be empty")
	}
	if len(name) > 15 { // Linux interface name limit
		return fmt.Errorf("interface name too long (max 15 characters)")
	}
	if !validInterfaceName.MatchString(name) {
		return fmt.Errorf("interface name contains invalid characters (allowed: a-z, A-Z, 0-9, _, -)")
	}
	return nil
}

const maxPeerNameLength = 64

// validatePeerName checks a display name: it is written as a comment line, so
// line breaks are the one thing it must not contain.
func validatePeerName(name string) error {
	if strings.ContainsAny(name, "\r\n") {
		return fmt.Errorf("peer name cannot contain line breaks")
	}
	if len(strings.TrimSpace(name)) > maxPeerNameLength {
		return fmt.Errorf("peer name too long (max %d characters)", maxPeerNameLength)
	}
	return nil
}

const maxHostLength = 253

// validateHost accepts a hostname or IP literal for client endpoints. An
// empty host is valid and means "detect the public IP".
func validateHost(host string) error {
	if host == "" {
		return nil
	}
	if len(host) > maxHostLength || strings.ContainsAny(host, " \t\r\n/") {
		return fmt.Errorf("invalid endpoint host %q", host)
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return fmt.Errorf("invalid endpoint host %q: a value with ':' must be an IP address", host)
	}
	return nil
}

// validateCIDRs trims, validates and de-duplicates a list of CIDRs, keeping
// the entries as typed. nil is returned for an empty list.
func validateCIDRs(values []string) ([]string, error) {
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(value); err != nil {
			return nil, fmt.Errorf("%q is not a CIDR", value)
		}
		if !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result, nil
}

func validateDNS(values []string) ([]string, error) {
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, " \t\r\n,") {
			return nil, fmt.Errorf("invalid DNS entry %q", value)
		}
		result = append(result, value)
	}
	return result, nil
}
