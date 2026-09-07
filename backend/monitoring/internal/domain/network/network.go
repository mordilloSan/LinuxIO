package network

type ConnectionStats struct {
	NetConnectionsEnabled bool           `json:"net_connections_enabled"`
	NFConntrackEnabled    bool           `json:"nf_conntrack_enabled"`
	Total                 int            `json:"total"`
	TCP                   int            `json:"tcp"`
	UDP                   int            `json:"udp"`
	Listen                int            `json:"listen,omitempty"`
	Established           int            `json:"established,omitempty"`
	SynSent               int            `json:"syn_sent,omitempty"`
	SynRecv               int            `json:"syn_recv,omitempty"`
	FinWait1              int            `json:"fin_wait1,omitempty"`
	FinWait2              int            `json:"fin_wait2,omitempty"`
	TimeWait              int            `json:"time_wait,omitempty"`
	Close                 int            `json:"close,omitempty"`
	CloseWait             int            `json:"close_wait,omitempty"`
	LastAck               int            `json:"last_ack,omitempty"`
	Closing               int            `json:"closing,omitempty"`
	Statuses              map[string]int `json:"statuses,omitempty"`
	NFConntrackCount      uint64         `json:"nf_conntrack_count,omitempty"`
	NFConntrackMax        uint64         `json:"nf_conntrack_max,omitempty"`
	NFConntrackPercent    float64        `json:"nf_conntrack_percent,omitempty"`
}

type IRQStat struct {
	IRQ         string   `json:"irq"`
	Total       uint64   `json:"total"`
	CPUCounts   []uint64 `json:"cpu_counts,omitempty"`
	Description string   `json:"description,omitempty"`
}
