package collector

import (
	"net"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
)

// 这些是内存里的伪文件系统，统计进去没有意义。
var pseudoFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "squashfs": true, "overlay": true,
	"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true,
	"devpts": true, "ramfs": true, "autofs": true, "mqueue": true,
	"debugfs": true, "tracefs": true, "securityfs": true, "pstore": true,
	"fusectl": true, "configfs": true, "binfmt_misc": true,
}

// Collect 采一次样。为了拿到有意义的 CPU 和网络速率，内部会阻塞 1 秒。
func Collect() (*Report, error) {
	r := &Report{Timestamp: time.Now()}

	if info, err := host.Info(); err == nil {
		r.Hostname = info.Hostname
		r.OS = strings.TrimSpace(info.Platform + " " + info.PlatformVersion)
		r.Kernel = info.KernelVersion
		r.Arch = info.KernelArch
		r.Uptime = info.Uptime
		r.BootTime = info.BootTime
	}

	if users, err := host.Users(); err == nil {
		for _, u := range users {
			r.Users = append(r.Users, u.User)
		}
	}

	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		r.CPUModel = infos[0].ModelName
	}
	if n, err := cpu.Counts(true); err == nil {
		r.CPUCores = n
	}
	if avg, err := load.Avg(); err == nil {
		r.LoadAvg = [3]float64{avg.Load1, avg.Load5, avg.Load15}
	}

	// 网络先取一次，等一秒再取一次算速率
	rx0, tx0 := netTotal()
	if pct, err := cpu.Percent(time.Second, false); err == nil && len(pct) > 0 {
		r.CPUPercent = round2(pct[0])
	}
	rx1, tx1 := netTotal()

	r.NetRx, r.NetTx = rx1, tx1
	if rx1 >= rx0 {
		r.NetRxRate = rx1 - rx0
	}
	if tx1 >= tx0 {
		r.NetTxRate = tx1 - tx0
	}

	if vm, err := mem.VirtualMemory(); err == nil {
		r.MemTotal = vm.Total
		r.MemUsed = vm.Used
	}
	if sw, err := mem.SwapMemory(); err == nil {
		r.SwapTotal = sw.Total
		r.SwapUsed = sw.Used
	}

	r.Disks = disks()
	r.IPs = ips()
	return r, nil
}

func netTotal() (uint64, uint64) {
	counters, err := gnet.IOCounters(false)
	if err != nil || len(counters) == 0 {
		return 0, 0
	}
	return counters[0].BytesRecv, counters[0].BytesSent
}

func disks() []DiskUsage {
	parts, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	var out []DiskUsage
	seen := map[string]bool{}
	for _, p := range parts {
		if pseudoFS[p.Fstype] || seen[p.Mountpoint] {
			continue
		}
		seen[p.Mountpoint] = true
		u, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}
		out = append(out, DiskUsage{
			Mount:  p.Mountpoint,
			FSType: p.Fstype,
			Total:  u.Total,
			Used:   u.Used,
		})
	}
	return out
}

func ips() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	return out
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
