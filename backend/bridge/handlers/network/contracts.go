package network

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

func networkInterfacesToAPI(values []NetworkInterfaceInfo) []apischema.NetworkInterface {
	result := make([]apischema.NetworkInterface, len(values))
	for i, value := range values {
		result[i] = apischema.NetworkInterface{
			Name: value.Name, Type: value.Type, MAC: value.MAC, MTU: int(value.MTU), Speed: value.Speed,
			Duplex: value.Duplex, State: int(value.State), IPv4: value.IP4Addresses, IPv6: value.IP6Addresses,
			RXSpeed: value.RxSpeed, TXSpeed: value.TxSpeed, DNS: value.DNS, Gateway: value.Gateway,
		}
		if value.IPv4Method != "" {
			result[i].IPv4Method = &value.IPv4Method
		}
	}
	return result
}
