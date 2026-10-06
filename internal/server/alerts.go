package server

import (
	"fmt"
	"log"
	"net/http"

	"serverpanel/internal/libvirt"
	"serverpanel/internal/model"
)

// alertStreak 记录某台虚拟机连续超阈值的次数。
type alertStreak struct {
	cpu  int
	mem  int
	disk int
}

func (s *Server) raiseAlert(kind, level string, hostID, serverID int64, target, message string, value float64) {
	a := &model.Alert{
		Kind: kind, Level: level, HostID: hostID, ServerID: serverID,
		Target: target, Message: message, Value: value,
	}
	created, err := s.st.OpenAlert(a)
	if err != nil {
		log.Println("写入告警失败:", err)
		return
	}
	if created {
		log.Printf("告警产生: %s", message)
	}
}

func (s *Server) resolveAlert(kind string, hostID, serverID int64) {
	ok, err := s.st.ResolveAlert(kind, hostID, serverID)
	if err != nil {
		log.Println("恢复告警失败:", err)
		return
	}
	if ok {
		log.Printf("告警恢复: kind=%s host=%d server=%d", kind, hostID, serverID)
	}
}

func pctOf(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	v := float64(used) * 100 / float64(total)
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func alertLevel(v float64) string {
	if v >= 95 {
		return "crit"
	}
	return "warn"
}

// evalAlerts 在每次采集后判定一台虚拟机是否触发或恢复告警。
func (s *Server) evalAlerts(h model.Host, m *LiveMetric) {
	if !s.cfg.AlertEnabled {
		return
	}
	consec := s.cfg.AlertConsecutive
	if consec < 1 {
		consec = 1
	}

	s.alertMu.Lock()
	st := s.alertStreak[m.ServerID]
	if st == nil {
		st = &alertStreak{}
		s.alertStreak[m.ServerID] = st
	}
	prevState := s.alertState[m.ServerID]
	s.alertState[m.ServerID] = m.State
	s.alertMu.Unlock()

	// 不在运行态：只在"从运行变成停止"的那一刻报一次
	if m.State != string(libvirt.Running) {
		if prevState == string(libvirt.Running) && s.cfg.AlertOnStop {
			s.raiseAlert(model.AlertStopped, "crit", h.ID, m.ServerID, m.Name,
				fmt.Sprintf("虚拟机 %s 已停止运行", m.Name), 0)
		}
		return
	}
	s.resolveAlert(model.AlertStopped, h.ID, m.ServerID)

	cpu := m.CPU
	mem := pctOf(m.MemUsedMB, m.MemTotalMB)
	disk := pctOf(m.DiskUsed, m.DiskTotal)

	// CPU
	if s.cfg.AlertCPU > 0 && cpu >= float64(s.cfg.AlertCPU) {
		st.cpu++
		if st.cpu >= consec {
			s.raiseAlert(model.AlertCPU, alertLevel(cpu), h.ID, m.ServerID, m.Name,
				fmt.Sprintf("虚拟机 %s CPU 持续 %.0f%%（阈值 %d%%）", m.Name, cpu, s.cfg.AlertCPU), cpu)
		}
	} else if st.cpu > 0 {
		st.cpu = 0
		s.resolveAlert(model.AlertCPU, h.ID, m.ServerID)
	}

	// 内存
	if s.cfg.AlertMem > 0 && mem >= float64(s.cfg.AlertMem) {
		st.mem++
		if st.mem >= consec {
			s.raiseAlert(model.AlertMem, alertLevel(mem), h.ID, m.ServerID, m.Name,
				fmt.Sprintf("虚拟机 %s 内存持续 %.0f%%（阈值 %d%%）", m.Name, mem, s.cfg.AlertMem), mem)
		}
	} else if st.mem > 0 {
		st.mem = 0
		s.resolveAlert(model.AlertMem, h.ID, m.ServerID)
	}

	// 磁盘
	if s.cfg.AlertDisk > 0 && disk >= float64(s.cfg.AlertDisk) {
		st.disk++
		if st.disk >= consec {
			s.raiseAlert(model.AlertDisk, alertLevel(disk), h.ID, m.ServerID, m.Name,
				fmt.Sprintf("虚拟机 %s 磁盘使用率 %.0f%%（阈值 %d%%）", m.Name, disk, s.cfg.AlertDisk), disk)
		}
	} else if st.disk > 0 {
		st.disk = 0
		s.resolveAlert(model.AlertDisk, h.ID, m.ServerID)
	}
}

// evalHostAlert 宿主机采集失败时告警，恢复后自动清除。
func (s *Server) evalHostAlert(h model.Host, offline bool) {
	if !s.cfg.AlertEnabled {
		return
	}
	if offline {
		s.raiseAlert(model.AlertHostOffline, "crit", h.ID, 0, h.Name,
			fmt.Sprintf("宿主机 %s 采集失败，可能与面板失联", h.Name), 0)
		return
	}
	s.resolveAlert(model.AlertHostOffline, h.ID, 0)
}

// canSeeAlert 判断当前用户是否有权看到这条告警。
func (s *Server) canSeeAlert(userID int64, role string, a model.Alert) bool {
	if role == model.RoleAdmin {
		return true
	}
	if a.HostID == 0 {
		return true
	}
	if ok, _ := s.st.IsBound(userID, a.HostID); ok {
		return true
	}
	if a.ServerID > 0 {
		ok, _ := s.st.IsServerBound(userID, a.ServerID)
		return ok
	}
	return false
}

// ---------- 接口 ----------

// handleAlerts 返回告警列表，未恢复的在前面。
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	all, err := s.st.Alerts(200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取告警失败")
		return
	}
	out := make([]model.Alert, 0, len(all))
	for _, a := range all {
		if s.canSeeAlert(c.UserID, c.Role, a) {
			out = append(out, a)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAlertSummary 给 App 轮询用的轻量接口：条数 + 最大 id + 最近未确认的几条。
func (s *Server) handleAlertSummary(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	all, err := s.st.Alerts(500)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取告警失败")
		return
	}

	active, unacked := 0, 0
	var maxID int64
	items := make([]model.Alert, 0, 5)
	for _, a := range all {
		if a.Status != "active" || !s.canSeeAlert(c.UserID, c.Role, a) {
			continue
		}
		active++
		if a.ID > maxID {
			maxID = a.ID
		}
		if a.AckAt == nil {
			unacked++
			if len(items) < 5 {
				items = append(items, a)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"active":  active,
		"unacked": unacked,
		"max_id":  maxID,
		"items":   items,
	})
}

// handleAckAlerts 确认告警：传 all 确认全部，或传 ids 确认指定几条。
func (s *Server) handleAckAlerts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	_ = decode(w, r, &req)

	if req.All {
		if err := s.st.AckAllActive(); err != nil {
			writeErr(w, http.StatusInternalServerError, "标记失败")
			return
		}
	} else {
		for _, id := range req.IDs {
			_ = s.st.AckAlert(id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
