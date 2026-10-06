package model

import "time"

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

// Host 对应一台 KVM 宿主机（通过 frp 映射出来的 SSH 端口访问）。
type Host struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	SSHHost    string    `json:"ssh_host"`
	SSHPort    int       `json:"ssh_port"`
	SSHUser    string    `json:"ssh_user"`
	LibvirtURI string    `json:"libvirt_uri"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// BindKey 是绑定用的 32 位密钥。
// ServerID 为 0 表示宿主级密钥（绑定后可管理该宿主下所有虚拟机）；
// 大于 0 表示虚拟机级密钥（只绑定那一台）。
type BindKey struct {
	ID        int64      `json:"id"`
	HostID    int64      `json:"host_id"`
	ServerID  int64      `json:"server_id"`
	Prefix    string     `json:"prefix"`
	KeyHash   string     `json:"-"`
	Status    string     `json:"status"` // active / revoked
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	RotatedAt *time.Time `json:"rotated_at"`
}

type UserHost struct {
	UserID  int64     `json:"user_id"`
	HostID  int64     `json:"host_id"`
	KeyID   int64     `json:"key_id"`
	BoundAt time.Time `json:"bound_at"`
}

// Server 是宿主上的虚拟机。
type Server struct {
	ID         int64  `json:"id"`
	HostID     int64  `json:"host_id"`
	Name       string `json:"name"`
	DomainName string `json:"domain_name"`
	SSHPort    int    `json:"ssh_port"`
	SSHUser    string `json:"ssh_user"`
	VCPU       int    `json:"vcpu"`
	MemMB      int    `json:"mem_mb"`
}

// Metric 是一次采集到的运行指标快照。
type Metric struct {
	ServerID   int64     `json:"server_id"`
	TS         time.Time `json:"ts"`
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
}

type AuditLog struct {
	ID       int64     `json:"id"`
	UserID   int64     `json:"user_id"`
	Username string    `json:"username"`
	HostID   int64     `json:"host_id"`
	ServerID int64     `json:"server_id"`
	Action   string    `json:"action"`
	Detail   string    `json:"detail"`
	Result   string    `json:"result"`
	IP       string    `json:"ip"`
	TS       time.Time `json:"ts"`
}

// 告警类型。
const (
	AlertCPU         = "cpu"          // 虚拟机 CPU 持续超阈值
	AlertMem         = "mem"          // 虚拟机内存持续超阈值
	AlertDisk        = "disk"         // 虚拟机磁盘超阈值
	AlertStopped     = "stopped"      // 虚拟机意外停止
	AlertHostOffline = "host_offline" // 宿主机采集失败
)

// Alert 是一条告警。同一类型在同一目标上只保留一条未恢复的记录，
// 持续命中时累加 Count 并刷新 UpdatedAt。
type Alert struct {
	ID         int64      `json:"id"`
	Kind       string     `json:"kind"`
	Level      string     `json:"level"` // warn / crit
	HostID     int64      `json:"host_id"`
	ServerID   int64      `json:"server_id"`
	Target     string     `json:"target"`
	Message    string     `json:"message"`
	Value      float64    `json:"value"`
	Status     string     `json:"status"` // active / resolved
	Count      int        `json:"count"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	AckAt      *time.Time `json:"ack_at"`
}
