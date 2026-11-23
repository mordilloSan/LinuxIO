package wireguard

import "encoding/binary"

// The few packets a connection check sends, built by hand so the check needs
// no command and no new module.

const (
	icmpEchoReply   = 0
	icmpEchoRequest = 8
)

// echoPayload marks the check's echo requests in a capture.
var echoPayload = []byte("linuxio wireguard check")

// echoRequest is an ICMP echo request; the kernel adds the IP header.
func echoRequest(id, seq uint16) []byte {
	msg := make([]byte, 8+len(echoPayload))
	msg[0] = icmpEchoRequest
	binary.BigEndian.PutUint16(msg[4:], id)
	binary.BigEndian.PutUint16(msg[6:], seq)
	copy(msg[8:], echoPayload)
	binary.BigEndian.PutUint16(msg[2:], icmpChecksum(msg))
	return msg
}

// isEchoReply reports whether msg, an ICMP message without its IP header, is
// an echo reply to one of the check's requests with this id.
func isEchoReply(msg []byte, id uint16) bool {
	return len(msg) >= 8 &&
		msg[0] == icmpEchoReply && msg[1] == 0 &&
		binary.BigEndian.Uint16(msg[4:]) == id &&
		icmpChecksum(msg) == 0
}

// icmpChecksum is the Internet checksum (RFC 1071). Over a message that
// carries its checksum it is 0.
func icmpChecksum(msg []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(msg); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(msg[i:]))
	}
	if len(msg)%2 == 1 {
		sum += uint32(msg[len(msg)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}

// dnsQuery asks for the root's name servers (". NS"), a question every
// resolver answers without a name of ours, with recursion desired.
func dnsQuery(id uint16) []byte {
	msg := make([]byte, 12+1+4)
	binary.BigEndian.PutUint16(msg[0:], id)
	binary.BigEndian.PutUint16(msg[2:], 0x0100) // RD
	binary.BigEndian.PutUint16(msg[4:], 1)      // QDCOUNT
	// msg[12] is the root name: a single zero length.
	binary.BigEndian.PutUint16(msg[13:], 2) // QTYPE NS
	binary.BigEndian.PutUint16(msg[15:], 1) // QCLASS IN
	return msg
}

// isDNSReply reports whether msg is a response to the query with this id.
// Any response counts, an error code too: the server answered through the
// tunnel.
func isDNSReply(msg []byte, id uint16) bool {
	return len(msg) >= 12 &&
		binary.BigEndian.Uint16(msg[0:]) == id &&
		msg[2]&0x80 != 0 && // QR: a response
		(msg[2]>>3)&0x0f == 0 // opcode QUERY
}
