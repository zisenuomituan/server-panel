package collector

import "time"

type DiskUsage struct {
	Mount  string `json:"mount"`
	FSType string `json:"fstype"`
	Total  uint64 `json:"total"`
	Used   uint64 `json:"used"`
}

// Report 是一台机器一次采集的结果。vm-collect 直接把这个结构序列化成 JSON。
type Report struct {
	Hostname string   `json:"hostname"`
	OS       string   `json:"os"`
	Kernel   string   `json:"kernel"`
	Arch     string   `json:"arch"`
	IPs      []string `json:"ips"`

	BootTime   uint64     `json:"boot_time"`
	Uptime     uint64     `json:"uptime"`
	LoadAvg    [3]float64 `json:"load_avg"`
	CPUPercent float64    `json:"cpu"`
	CPUCores   int        `json:"cpu_cores"`
	CPUModel   string     `json:"cpu_model"`

	MemTotal  uint64 `json:"mem_total"`
	MemUsed   uint64 `json:"mem_used"`
	SwapTotal uint64 `json:"swap_total"`
	SwapUsed  uint64 `json:"swap_used"`

	Disks []DiskUsage `json:"disks"`

	// 网络累计字节与实际收发速率（字节/秒）
	NetRx     uint64 `json:"net_rx"`
	NetTx     uint64 `json:"net_tx"`
	NetRxRate uint64 `json:"net_rx_rate"`
	NetTxRate uint64 `json:"net_tx_rate"`

	Users     []string  `json:"users"`
	Timestamp time.Time `json:"timestamp"`
}
