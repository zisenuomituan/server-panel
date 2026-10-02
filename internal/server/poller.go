package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"serverpanel/internal/collector"
	"serverpanel/internal/libvirt"
	"serverpanel/internal/model"
)

const collectCmd = "vm-collect"

func (s *Server) RunPoller(ctx context.Context) {
	interval := time.Duration(s.cfg.PollSeconds) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}

	s.pollOnce(ctx)

	tick := time.NewTicker(interval)
	prune := time.NewTicker(time.Hour)
	defer tick.Stop()
	defer prune.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.pollOnce(ctx)
		case <-prune.C:
			_ = s.st.PruneMetrics(24 * time.Hour)
		}
	}
}

func (s *Server) pollOnce(ctx context.Context) {
	hosts, err := s.st.Hosts()
	if err != nil {
		log.Println("读取宿主机失败:", err)
		return
	}

	for _, h := range hosts {
		backend := s.backendFor(hostConn{Host: h.SSHHost, Port: h.SSHPort, User: h.SSHUser, URI: h.LibvirtURI})

		domains, err := backend.List(ctx)
		if err != nil {
			_ = s.st.SetHostStatus(h.ID, "offline")
			continue
		}
		_ = s.st.SetHostStatus(h.ID, "online")

		for _, d := range domains {
			sv := &model.Server{
				HostID:     h.ID,
				Name:       d.Name,
				DomainName: d.Name,
				VCPU:       d.VCPU,
				MemMB:      d.MemMB,
			}
			if err := s.st.UpsertServer(sv); err != nil {
				log.Printf("保存虚拟机 %s 失败: %v", d.Name, err)
				continue
			}
			sv, err = s.st.ServerByID(sv.ID)
			if err != nil {
				continue
			}

			m := s.collectServer(ctx, h, sv, backend, d)
			if m == nil {
				continue
			}
			m.HostID = h.ID
			m.ServerID = sv.ID
			m.Name = sv.Name
			m.State = string(d.State)

			_ = s.st.AddMetric(&model.Metric{
				ServerID:   m.ServerID,
				TS:         m.Updated,
				CPU:        m.CPU,
				MemUsedMB:  m.MemUsedMB,
				MemTotalMB: m.MemTotalMB,
				DiskUsed:   m.DiskUsed,
				DiskTotal:  m.DiskTotal,
				NetRxRate:  m.NetRxRate,
				NetTxRate:  m.NetTxRate,
				NetRx:      m.NetRx,
				NetTx:      m.NetTx,
				Uptime:     m.Uptime,
			})

			s.setLive(m)
			s.hub.broadcast(s, m)
		}
	}
}

// collectServer 优先走 guest 内采集（VM 有独立 frp SSH 端口时），否则退回 libvirt。
func (s *Server) collectServer(ctx context.Context, h model.Host, sv *model.Server, backend libvirt.Backend, d libvirt.Domain) *LiveMetric {
	if sv.SSHPort > 0 && s.pool != nil {
		report, err := s.collectGuest(ctx, h, sv)
		if err == nil {
			return metricFromReport(report)
		}
		log.Printf("guest 采集 %s 失败，改用 libvirt: %v", sv.Name, err)
	}

	stats, err := backend.Stats(ctx, d.Name)
	if err != nil {
		return nil
	}
	return s.metricFromStats(h.ID, d, stats)
}

func (s *Server) collectGuest(ctx context.Context, h model.Host, sv *model.Server) (*collector.Report, error) {
	user := sv.SSHUser
	if user == "" {
		user = "root"
	}
	addr := fmt.Sprintf("%s:%d", h.SSHHost, sv.SSHPort)
	out, err := s.pool.Run(ctx, addr, user, collectCmd)
	if err != nil {
		return nil, err
	}
	var report collector.Report
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		return nil, fmt.Errorf("解析指标失败: %w", err)
	}
	return &report, nil
}

func metricFromReport(r *collector.Report) *LiveMetric {
	var diskUsed, diskTotal uint64
	disks := make([]DiskInfo, 0, len(r.Disks))
	for _, d := range r.Disks {
		diskUsed += d.Used
		diskTotal += d.Total
		disks = append(disks, DiskInfo{Mount: d.Mount, Total: d.Total, Used: d.Used})
	}
	return &LiveMetric{
		State:      string(libvirt.Running),
		CPU:        r.CPUPercent,
		MemUsedMB:  int64(r.MemUsed / 1024 / 1024),
		MemTotalMB: int64(r.MemTotal / 1024 / 1024),
		DiskUsed:   int64(diskUsed),
		DiskTotal:  int64(diskTotal),
		NetRxRate:  int64(r.NetRxRate),
		NetTxRate:  int64(r.NetTxRate),
		NetRx:      int64(r.NetRx),
		NetTx:      int64(r.NetTx),
		Uptime:     int64(r.Uptime),
		Updated:    time.Now(),
		Hostname:   r.Hostname,
		OS:         r.OS,
		Kernel:     r.Kernel,
		Arch:       r.Arch,
		IPs:        r.IPs,
		Users:      r.Users,
		Disks:      disks,
	}
}

func (s *Server) metricFromStats(hostID int64, d libvirt.Domain, stats libvirt.Stats) *LiveMetric {
	key := fmt.Sprintf("%d/%s", hostID, d.Name)
	now := time.Now()

	m := &LiveMetric{
		MemUsedMB:  stats.MemUsedMB,
		MemTotalMB: stats.MemTotalMB,
		NetRx:      int64(stats.NetRx),
		NetTx:      int64(stats.NetTx),
		Updated:    now,
	}

	if prev, ok := s.cpuPrev[key]; ok {
		dt := now.Sub(prev.at).Seconds()
		if dt > 0 {
			cores := prev.vcpu
			if cores == 0 {
				cores = 1
			}
			used := stats.CPUSeconds - prev.seconds
			if used < 0 {
				used = 0
			}
			m.CPU = clampPct(used / (dt * float64(cores)) * 100)

			if stats.NetRx >= prev.netRx {
				m.NetRxRate = int64(float64(stats.NetRx-prev.netRx) / dt)
			}
			if stats.NetTx >= prev.netTx {
				m.NetTxRate = int64(float64(stats.NetTx-prev.netTx) / dt)
			}
		}
	}

	s.mu.Lock()
	s.cpuPrev[key] = cpuSample{
		seconds: stats.CPUSeconds,
		netRx:   stats.NetRx,
		netTx:   stats.NetTx,
		at:      now,
		vcpu:    d.VCPU,
	}
	s.mu.Unlock()

	return m
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return float64(int(v*100+0.5)) / 100
}

func (s *Server) setLive(m *LiveMetric) {
	s.mu.Lock()
	s.live[m.ServerID] = m
	s.mu.Unlock()
}

func (s *Server) snapshot() []*LiveMetric {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*LiveMetric, 0, len(s.live))
	for _, m := range s.live {
		out = append(out, m)
	}
	return out
}

func (s *Server) liveFor(serverID int64) *LiveMetric {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live[serverID]
}
