package shares

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

func nfsClients(clients []apischema.NFSClient) []NFSClient {
	out := make([]NFSClient, 0, len(clients))
	for _, client := range clients {
		out = append(out, NFSClient{Host: client.Host, Options: client.Options})
	}
	return out
}
