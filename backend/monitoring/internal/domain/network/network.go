package network

type ConnectionStats struct {
	NetConnectionsEnabled bool           `json:"net_connections_enabled"`
	NFConntrackEnabled    bool           `json:"nf_conntrack_enabled"`
	Total                 int            `json:"total"`
	TCP                   int            `json:"tcp"`
	UDP                   int            `json:"udp"`
	Listen                int            `json:"listen,omitzero"`
	Established           int            `json:"established,omitzero"`
	SynSent               int            `json:"syn_sent,omitzero"`
	SynRecv               int            `json:"syn_recv,omitzero"`
	FinWait1              int            `json:"fin_wait1,omitzero"`
	FinWait2              int            `json:"fin_wait2,omitzero"`
	TimeWait              int            `json:"time_wait,omitzero"`
	Close                 int            `json:"close,omitzero"`
	CloseWait             int            `json:"close_wait,omitzero"`
	LastAck               int            `json:"last_ack,omitzero"`
	Closing               int            `json:"closing,omitzero"`
	Statuses              map[string]int `json:"statuses,omitempty"`
	NFConntrackCount      uint64         `json:"nf_conntrack_count,omitzero"`
	NFConntrackMax        uint64         `json:"nf_conntrack_max,omitzero"`
	NFConntrackPercent    float64        `json:"nf_conntrack_percent,omitzero"`
}

type IRQStat struct {
	IRQ         string   `json:"irq"`
	Total       uint64   `json:"total"`
	CPUCounts   []uint64 `json:"cpu_counts,omitempty"`
	Description string   `json:"description,omitempty"`
}
