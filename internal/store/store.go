package store

import (
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"

	"serverpanel/internal/model"
)

var ErrNotFound = errors.New("记录不存在")

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	// 忽略失败：多数情况是列已经存在
	for _, m := range migrations {
		_, _ = db.Exec(m)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---------- users ----------

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(u *model.User) error {
	res, err := s.db.Exec(
		`INSERT INTO users (username, email, password_hash, role) VALUES (?, ?, ?, ?)`,
		u.Username, u.Email, u.PasswordHash, u.Role,
	)
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UserByName(name string) (*model.User, error) {
	row := s.db.QueryRow(
		`SELECT id, username, email, password_hash, role, created_at FROM users WHERE username = ?`, name)
	return scanUser(row)
}

func (s *Store) UpdatePassword(userID int64, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
	return err
}

func (s *Store) UserByID(id int64) (*model.User, error) {
	row := s.db.QueryRow(
		`SELECT id, username, email, password_hash, role, created_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func scanUser(row *sql.Row) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

// ---------- hosts ----------

func (s *Store) CreateHost(h *model.Host) error {
	res, err := s.db.Exec(
		`INSERT INTO hosts (name, ssh_host, ssh_port, ssh_user, libvirt_uri, status) VALUES (?, ?, ?, ?, ?, ?)`,
		h.Name, h.SSHHost, h.SSHPort, h.SSHUser, h.LibvirtURI, "unknown",
	)
	if err != nil {
		return err
	}
	h.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) HostByID(id int64) (*model.Host, error) {
	row := s.db.QueryRow(
		`SELECT id, name, ssh_host, ssh_port, ssh_user, libvirt_uri, status, created_at FROM hosts WHERE id = ?`, id)
	return scanHost(row)
}

func (s *Store) Hosts() ([]model.Host, error) {
	rows, err := s.db.Query(
		`SELECT id, name, ssh_host, ssh_port, ssh_user, libvirt_uri, status, created_at FROM hosts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Host
	for rows.Next() {
		var h model.Host
		if err := rows.Scan(&h.ID, &h.Name, &h.SSHHost, &h.SSHPort, &h.SSHUser, &h.LibvirtURI, &h.Status, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) DeleteHost(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		`DELETE FROM metrics WHERE server_id IN (SELECT id FROM servers WHERE host_id = ?)`,
		`DELETE FROM user_servers WHERE server_id IN (SELECT id FROM servers WHERE host_id = ?)`,
		`DELETE FROM servers WHERE host_id = ?`,
		`DELETE FROM user_hosts WHERE host_id = ?`,
		`DELETE FROM bind_keys WHERE host_id = ?`,
		`DELETE FROM hosts WHERE id = ?`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetHostStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE hosts SET status = ? WHERE id = ?`, status, id)
	return err
}

// HostsForUser 返回该账号绑定过的所有宿主。
func (s *Store) HostsForUser(userID int64) ([]model.Host, error) {
	rows, err := s.db.Query(`
		SELECT h.id, h.name, h.ssh_host, h.ssh_port, h.ssh_user, h.libvirt_uri, h.status, h.created_at
		FROM hosts h
		JOIN user_hosts uh ON uh.host_id = h.id
		WHERE uh.user_id = ?
		ORDER BY h.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Host
	for rows.Next() {
		var h model.Host
		if err := rows.Scan(&h.ID, &h.Name, &h.SSHHost, &h.SSHPort, &h.SSHUser, &h.LibvirtURI, &h.Status, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func scanHost(row *sql.Row) (*model.Host, error) {
	var h model.Host
	err := row.Scan(&h.ID, &h.Name, &h.SSHHost, &h.SSHPort, &h.SSHUser, &h.LibvirtURI, &h.Status, &h.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &h, err
}

// ---------- bind keys ----------

func (s *Store) CreateBindKey(k *model.BindKey) error {
	res, err := s.db.Exec(
		`INSERT INTO bind_keys (host_id, server_id, prefix, key_hash, status, expires_at) VALUES (?, ?, ?, ?, 'active', ?)`,
		k.HostID, k.ServerID, k.Prefix, k.KeyHash, k.ExpiresAt,
	)
	if err != nil {
		return err
	}
	k.ID, _ = res.LastInsertId()
	return nil
}

const bindKeyCols = `id, host_id, server_id, prefix, key_hash, status, created_at, expires_at, rotated_at`

func scanBindKey(scan func(...any) error) (*model.BindKey, error) {
	var k model.BindKey
	err := scan(&k.ID, &k.HostID, &k.ServerID, &k.Prefix, &k.KeyHash, &k.Status, &k.CreatedAt, &k.ExpiresAt, &k.RotatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &k, err
}

// FindActiveKey 按哈希找一把当前可用（未撤销且未过期）的密钥。
func (s *Store) FindActiveKey(hash string) (*model.BindKey, error) {
	row := s.db.QueryRow(`
		SELECT `+bindKeyCols+`
		FROM bind_keys
		WHERE key_hash = ? AND status = 'active'
		  AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)`, hash)
	return scanBindKey(row.Scan)
}

// KeysForHost 返回某台宿主的所有密钥（含宿主级和它下面虚拟机的密钥）。
func (s *Store) KeysForHost(hostID int64) ([]model.BindKey, error) {
	rows, err := s.db.Query(`SELECT `+bindKeyCols+` FROM bind_keys WHERE host_id = ? ORDER BY id DESC`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectKeys(rows)
}

func (s *Store) KeysForServer(serverID int64) ([]model.BindKey, error) {
	rows, err := s.db.Query(`SELECT `+bindKeyCols+` FROM bind_keys WHERE server_id = ? ORDER BY id DESC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectKeys(rows)
}

func collectKeys(rows *sql.Rows) ([]model.BindKey, error) {
	var out []model.BindKey
	for rows.Next() {
		var k model.BindKey
		if err := rows.Scan(&k.ID, &k.HostID, &k.ServerID, &k.Prefix, &k.KeyHash, &k.Status, &k.CreatedAt, &k.ExpiresAt, &k.RotatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) RevokeKey(id int64) error {
	_, err := s.db.Exec(`UPDATE bind_keys SET status = 'revoked', rotated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

// RevokeKeysForHost 只撤销宿主级密钥（server_id = 0），不影响单台虚拟机的密钥。
func (s *Store) RevokeKeysForHost(hostID int64) error {
	_, err := s.db.Exec(`UPDATE bind_keys SET status = 'revoked', rotated_at = CURRENT_TIMESTAMP WHERE host_id = ? AND server_id = 0 AND status = 'active'`, hostID)
	return err
}

func (s *Store) RevokeKeysForServer(serverID int64) error {
	_, err := s.db.Exec(`UPDATE bind_keys SET status = 'revoked', rotated_at = CURRENT_TIMESTAMP WHERE server_id = ? AND status = 'active'`, serverID)
	return err
}

// ---------- user_hosts ----------

func (s *Store) Bind(userID, hostID, keyID int64) error {
	_, err := s.db.Exec(`
		INSERT INTO user_hosts (user_id, host_id, key_id) VALUES (?, ?, ?)
		ON CONFLICT(user_id, host_id) DO UPDATE SET key_id = excluded.key_id,
			bound_at = CURRENT_TIMESTAMP`,
		userID, hostID, keyID)
	return err
}

func (s *Store) Unbind(userID, hostID int64) error {
	_, err := s.db.Exec(`DELETE FROM user_hosts WHERE user_id = ? AND host_id = ?`, userID, hostID)
	return err
}

func (s *Store) IsBound(userID, hostID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM user_hosts WHERE user_id = ? AND host_id = ?`, userID, hostID).Scan(&n)
	return n > 0, err
}

// UnbindHostAndServers 解绑宿主时，连同它下面用户的单机绑定一起清掉。
func (s *Store) UnbindHostAndServers(userID, hostID int64) error {
	if _, err := s.db.Exec(`DELETE FROM user_hosts WHERE user_id = ? AND host_id = ?`, userID, hostID); err != nil {
		return err
	}
	_, err := s.db.Exec(`
		DELETE FROM user_servers
		WHERE user_id = ? AND server_id IN (SELECT id FROM servers WHERE host_id = ?)`, userID, hostID)
	return err
}

func (s *Store) BindServer(userID, serverID, keyID int64) error {
	_, err := s.db.Exec(`
		INSERT INTO user_servers (user_id, server_id, key_id) VALUES (?, ?, ?)
		ON CONFLICT(user_id, server_id) DO UPDATE SET key_id = excluded.key_id,
			bound_at = CURRENT_TIMESTAMP`, userID, serverID, keyID)
	return err
}

func (s *Store) IsServerBound(userID, serverID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM user_servers WHERE user_id = ? AND server_id = ?`, userID, serverID).Scan(&n)
	return n > 0, err
}

// ServersForUser 返回用户单独绑定的虚拟机。
func (s *Store) ServersForUser(userID int64) ([]model.Server, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.host_id, s.name, s.domain_name, s.ssh_port, s.ssh_user, s.vcpu, s.mem_mb
		FROM servers s
		JOIN user_servers us ON us.server_id = s.id
		WHERE us.user_id = ?
		ORDER BY s.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Server
	for rows.Next() {
		var sv model.Server
		if err := rows.Scan(&sv.ID, &sv.HostID, &sv.Name, &sv.DomainName, &sv.SSHPort, &sv.SSHUser, &sv.VCPU, &sv.MemMB); err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, rows.Err()
}

// ---------- servers ----------

func (s *Store) UpsertServer(sv *model.Server) error {
	// 用 upsert 保持规格同步。注意不能依赖 LastInsertId：冲突走 UPDATE 时它可能
	// 返回上一行的 id，导致把指标写到别的机器上，所以统一回查一次。
	_, err := s.db.Exec(`
		INSERT INTO servers (host_id, name, domain_name, ssh_port, ssh_user, vcpu, mem_mb)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(host_id, domain_name) DO UPDATE SET
			name = excluded.name, vcpu = excluded.vcpu, mem_mb = excluded.mem_mb`,
		sv.HostID, sv.Name, sv.DomainName, sv.SSHPort, sv.SSHUser, sv.VCPU, sv.MemMB)
	if err != nil {
		return err
	}
	return s.db.QueryRow(`SELECT id FROM servers WHERE host_id = ? AND domain_name = ?`,
		sv.HostID, sv.DomainName).Scan(&sv.ID)
}

func (s *Store) ServersByHost(hostID int64) ([]model.Server, error) {
	rows, err := s.db.Query(`
		SELECT id, host_id, name, domain_name, ssh_port, ssh_user, vcpu, mem_mb
		FROM servers WHERE host_id = ? ORDER BY name`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Server
	for rows.Next() {
		var sv model.Server
		if err := rows.Scan(&sv.ID, &sv.HostID, &sv.Name, &sv.DomainName, &sv.SSHPort, &sv.SSHUser, &sv.VCPU, &sv.MemMB); err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, rows.Err()
}

func (s *Store) ServerByID(id int64) (*model.Server, error) {
	row := s.db.QueryRow(`
		SELECT id, host_id, name, domain_name, ssh_port, ssh_user, vcpu, mem_mb
		FROM servers WHERE id = ?`, id)

	var sv model.Server
	err := row.Scan(&sv.ID, &sv.HostID, &sv.Name, &sv.DomainName, &sv.SSHPort, &sv.SSHUser, &sv.VCPU, &sv.MemMB)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &sv, err
}

// ---------- metrics ----------

func (s *Store) AddMetric(m *model.Metric) error {
	_, err := s.db.Exec(`
		INSERT INTO metrics (server_id, ts, cpu, mem_used_mb, mem_total_mb, disk_used, disk_total,
			net_rx_rate, net_tx_rate, net_rx, net_tx, uptime)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ServerID, m.TS, m.CPU, m.MemUsedMB, m.MemTotalMB, m.DiskUsed, m.DiskTotal,
		m.NetRxRate, m.NetTxRate, m.NetRx, m.NetTx, m.Uptime)
	return err
}

func (s *Store) History(serverID int64, limit int) ([]model.Metric, error) {
	rows, err := s.db.Query(`
		SELECT server_id, ts, cpu, mem_used_mb, mem_total_mb, disk_used, disk_total,
			net_rx_rate, net_tx_rate, net_rx, net_tx, uptime
		FROM metrics WHERE server_id = ? ORDER BY ts DESC LIMIT ?`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Metric
	for rows.Next() {
		var m model.Metric
		if err := rows.Scan(&m.ServerID, &m.TS, &m.CPU, &m.MemUsedMB, &m.MemTotalMB, &m.DiskUsed, &m.DiskTotal,
			&m.NetRxRate, &m.NetTxRate, &m.NetRx, &m.NetTx, &m.Uptime); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PruneMetrics 只保留最近一段时间的原始点，避免库无限膨胀。
func (s *Store) PruneMetrics(keep time.Duration) error {
	cutoff := time.Now().Add(-keep)
	_, err := s.db.Exec(`DELETE FROM metrics WHERE ts < ?`, cutoff)
	return err
}

// ---------- audit ----------

func (s *Store) AddAudit(a *model.AuditLog) error {
	_, err := s.db.Exec(`
		INSERT INTO audit_logs (user_id, host_id, server_id, action, detail, result, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.UserID, a.HostID, a.ServerID, a.Action, a.Detail, a.Result, a.IP)
	return err
}

func (s *Store) Audits(limit int) ([]model.AuditLog, error) {
	rows, err := s.db.Query(`
		SELECT l.id, l.user_id, COALESCE(u.username, ''), l.host_id, l.server_id,
		       l.action, l.detail, l.result, l.ip, l.ts
		FROM audit_logs l
		LEFT JOIN users u ON u.id = l.user_id
		ORDER BY l.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.AuditLog
	for rows.Next() {
		var a model.AuditLog
		if err := rows.Scan(&a.ID, &a.UserID, &a.Username, &a.HostID, &a.ServerID,
			&a.Action, &a.Detail, &a.Result, &a.IP, &a.TS); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AuditsForUser(userID int64, limit int) ([]model.AuditLog, error) {
	rows, err := s.db.Query(`
		SELECT l.id, l.user_id, COALESCE(u.username, ''), l.host_id, l.server_id,
		       l.action, l.detail, l.result, l.ip, l.ts
		FROM audit_logs l
		LEFT JOIN users u ON u.id = l.user_id
		WHERE l.user_id = ?
		ORDER BY l.id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.AuditLog
	for rows.Next() {
		var a model.AuditLog
		if err := rows.Scan(&a.ID, &a.UserID, &a.Username, &a.HostID, &a.ServerID,
			&a.Action, &a.Detail, &a.Result, &a.IP, &a.TS); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
