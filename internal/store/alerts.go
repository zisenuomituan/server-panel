package store

import (
	"database/sql"
	"errors"
	"time"

	"serverpanel/internal/model"
)

const alertCols = `id, kind, level, host_id, server_id, target, message, value, status, count, created_at, updated_at, resolved_at, ack_at`

func scanAlert(scan func(...any) error) (*model.Alert, error) {
	var a model.Alert
	err := scan(&a.ID, &a.Kind, &a.Level, &a.HostID, &a.ServerID, &a.Target, &a.Message,
		&a.Value, &a.Status, &a.Count, &a.CreatedAt, &a.UpdatedAt, &a.ResolvedAt, &a.AckAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

// OpenAlert 记录一条告警。同一类型在同一目标上已经有未恢复的记录时，
// 就地在原记录上累加次数、刷新最新数值，避免告警风暴。返回是否新建。
func (s *Store) OpenAlert(a *model.Alert) (bool, error) {
	row := s.db.QueryRow(`
		SELECT `+alertCols+`
		FROM alerts
		WHERE kind = ? AND host_id = ? AND server_id = ? AND status = 'active'`,
		a.Kind, a.HostID, a.ServerID)

	existing, err := scanAlert(row.Scan)
	if err == nil && existing != nil {
		_, err = s.db.Exec(`
			UPDATE alerts
			SET count = count + 1, value = ?, message = ?, level = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`,
			a.Value, a.Message, a.Level, existing.ID)
		a.ID = existing.ID
		a.Count = existing.Count + 1
		a.CreatedAt = existing.CreatedAt
		a.Status = existing.Status
		a.AckAt = existing.AckAt
		return false, err
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}

	res, err := s.db.Exec(`
		INSERT INTO alerts (kind, level, host_id, server_id, target, message, value)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.Kind, a.Level, a.HostID, a.ServerID, a.Target, a.Message, a.Value)
	if err != nil {
		return false, err
	}
	a.ID, _ = res.LastInsertId()
	a.Count = 1
	a.Status = "active"
	return true, nil
}

// ResolveAlert 把某个目标上的某类告警标记为已恢复，返回是否确实有告警被恢复。
func (s *Store) ResolveAlert(kind string, hostID, serverID int64) (bool, error) {
	res, err := s.db.Exec(`
		UPDATE alerts
		SET status = 'resolved', resolved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE kind = ? AND host_id = ? AND server_id = ? AND status = 'active'`,
		kind, hostID, serverID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Alerts 返回告警列表，未恢复的排在前面。
func (s *Store) Alerts(limit int) ([]model.Alert, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT `+alertCols+`
		FROM alerts
		ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END, id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Alert
	for rows.Next() {
		a, err := scanAlert(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// LatestUnacked 取最近几条未确认的未恢复告警，给 App 生成通知用。
func (s *Store) LatestUnacked(limit int) ([]model.Alert, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.Query(`SELECT `+alertCols+`
		FROM alerts
		WHERE status = 'active' AND ack_at IS NULL
		ORDER BY id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Alert
	for rows.Next() {
		a, err := scanAlert(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// AlertStats 返回未恢复条数、其中未确认的条数、以及当前最大告警 id。
func (s *Store) AlertStats() (active, unacked int, maxID int64, err error) {
	err = s.db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN ack_at IS NULL THEN 1 ELSE 0 END), 0),
		       COALESCE(MAX(id), 0)
		FROM alerts WHERE status = 'active'`).Scan(&active, &unacked, &maxID)
	return
}

// AckAlert 确认一条告警。
func (s *Store) AckAlert(id int64) error {
	_, err := s.db.Exec(`UPDATE alerts SET ack_at = CURRENT_TIMESTAMP WHERE id = ? AND ack_at IS NULL`, id)
	return err
}

// AckAllActive 确认当前所有未恢复的告警。
func (s *Store) AckAllActive() error {
	_, err := s.db.Exec(`UPDATE alerts SET ack_at = CURRENT_TIMESTAMP WHERE status = 'active' AND ack_at IS NULL`)
	return err
}

// PruneAlerts 清理已恢复且超过保留期的告警，避免库无限膨胀。
func (s *Store) PruneAlerts(keep time.Duration) error {
	cutoff := time.Now().Add(-keep)
	_, err := s.db.Exec(`DELETE FROM alerts WHERE status = 'resolved' AND resolved_at IS NOT NULL AND resolved_at < ?`, cutoff)
	return err
}
