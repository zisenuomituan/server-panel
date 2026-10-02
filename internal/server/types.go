package server

import "time"

// DiskInfo 是某个挂载点的用量，用于详情页展开显示。
type DiskInfo struct {
	Mount string `json:"mount"`
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

// LiveMetric 是推给前端的一台机器的最新状态。
type LiveMetric struct {
	ServerID   int64     `json:"server_id"`
	HostID     int64     `json:"host_id"`
	Name       string    `json:"name"`
	State      string    `json:"state"`
	CPU        float64   `json:"cpu"`
	MemUsedMB  int64     `json:"mem_used_mb"`
	MemTotalMB int64     `json:"mem_total_mb"`
	DiskUsed   int64     `json:"disk_used"`
	DiskTotal  int64     `json:"disk_total"`
	NetRxRate  int64     `json:"net_rx_rate"`
	NetTxRate  int64     `json:"net_tx_rate"`
	NetRx      int64     `json:"net_rx"`
	NetTx      int64     `json:"net_tx"`
	Uptime     int64     `json:"uptime"`
	Updated    time.Time `json:"updated"`

	// 以下来自 guest 内采集，libvirt 路径下为空
	Hostname string     `json:"hostname,omitempty"`
	OS       string     `json:"os,omitempty"`
	Kernel   string     `json:"kernel,omitempty"`
	Arch     string     `json:"arch,omitempty"`
	IPs      []string   `json:"ips,omitempty"`
	Users    []string   `json:"users,omitempty"`
	Disks    []DiskInfo `json:"disks,omitempty"`
}

// cpuSample 用上次采集的时间点，把 cpu.time / 网络的累计值换算成速率。
type cpuSample struct {
	seconds float64
	netRx   uint64
	netTx   uint64
	at      time.Time
	vcpu    int
}

type wsMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}
