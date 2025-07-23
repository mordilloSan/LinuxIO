package upnp

import (
	"fmt"
	"backend/internal/logger"
	"backend/internal/utils"
	"net/url"

	"github.com/huin/goupnp/dcps/internetgateway1"
)

// discoverIgdDescriptorUrl returns the IGD device descriptor URL (desc: ...gatedesc0b.xml).
// Returns "" if not found.
func DiscoverIgdDescriptorUrl() string {
	clients, _, err := internetgateway1.NewWANIPConnection1Clients()
	if err != nil || len(clients) == 0 {
		logger.Warnf(" UPnP: IGD descriptor discovery failed: %v", err)
		return ""
	}
	for _, c := range clients {
		if c == nil || c.Location == nil {
			continue
		}
		descUrl := c.Location.String()
		logger.Infof(" UPnP: Discovered IGD descriptor URL: %s", descUrl)
		if descUrl != "" {
			return descUrl
		}
	}
	return ""
}

func UpnpAddPortWithCheck(igdUrl, internalIP string, port int, desc string) error {
	parsed, err := url.Parse(igdUrl)
	if err != nil {
		return fmt.Errorf("invalid IGD URL: %w", err)
	}

	devs, err := internetgateway1.NewWANIPConnection1ClientsByURL(parsed)
	if err != nil || len(devs) == 0 {
		return fmt.Errorf("failed to connect to IGD at %s: %w", igdUrl, err)
	}
	client := devs[0]
	externalPort := uint16(port)
	internalPort := uint16(port)
	protocol := "UDP"

	// Check if mapping already exists
	_, mappedClient, mappedEnabled, mappedDesc, mappedLease, existsErr := client.GetSpecificPortMappingEntry(
		"", externalPort, protocol,
	)
	if existsErr == nil {
		if mappedClient == internalIP && mappedEnabled {
			logger.Infof(" UPnP: Port %d/%s is already forwarded to correct IP %s (desc: %s) for %s (lease: %ds)", port, protocol, mappedClient, mappedDesc, internalIP, mappedLease)
			return nil // Already mapped to the correct internal IP
		} else {
			logger.Infof(" UPnP: Port %d/%s is mapped to %s, expected %s. Deleting and recreating.", port, protocol, mappedClient, internalIP)
			// Delete existing mapping
			delErr := client.DeletePortMapping("", externalPort, protocol)
			if delErr != nil {
				return fmt.Errorf("failed to delete old mapping for port %d/%s: %w", port, protocol, delErr)
			}
			// Continue to add new mapping below
		}
	} else {
		logger.Infof(" UPnP: Port %d/%s is not forwarded, proceeding to add", port, protocol)
	}

	leaseDuration := uint32(0) // 0 = permanent

	err = client.AddPortMapping(
		"",            // NewRemoteHost
		externalPort,  // NewExternalPort
		protocol,      // NewProtocol
		internalPort,  // NewInternalPort
		internalIP,    // NewInternalClient
		true,          // NewEnabled
		desc,          // NewPortMappingDescription
		leaseDuration, // NewLeaseDuration
	)
	if err != nil {
		return fmt.Errorf("AddPortMapping failed: %w", err)
	}
	logger.Infof("UPnP port mapping succeeded for UDP %d", externalPort)
	return nil
}

func OpenRouterPort(egressNic string, listenPort int, name string) {
	internalIP, err := utils.GetLocalIPByInterface(egressNic)
	if err != nil {
		logger.Warnf(" Failed to get local IP for %s: %v", egressNic, err)
	}
	if internalIP != "" {
		igdUrl := DiscoverIgdDescriptorUrl()
		logger.Debugf(" UPnP: Using IGD URL: %s", igdUrl)
		err = UpnpAddPortWithCheck(igdUrl, internalIP, listenPort, "WireGuard "+name)
		if err != nil {
			logger.Warnf(" UPnP port mapping failed: %v", err)
		}
	} else {
		logger.Warnf(" UPnP port mapping skipped: could not determine local IP")
	}
}
