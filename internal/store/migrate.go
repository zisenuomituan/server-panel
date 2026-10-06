package store

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	username      TEXT NOT NULL UNIQUE,
	email         TEXT NOT NULL DEFAULT '',
	password_hash TEXT NOT NULL,
	role          TEXT NOT NULL DEFAULT 'operator',
	created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS hosts (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT NOT NULL,
	ssh_host    TEXT NOT NULL,
	ssh_port    INTEGER NOT NULL,
	ssh_user    TEXT NOT NULL DEFAULT 'panel',
	libvirt_uri TEXT NOT NULL DEFAULT 'qemu:///system',
	status      TEXT NOT NULL DEFAULT 'unknown',
	created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bind_keys (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	host_id    INTEGER NOT NULL,
	server_id  INTEGER NOT NULL DEFAULT 0,
	prefix     TEXT NOT NULL,
	key_hash   TEXT NOT NULL UNIQUE,
	status     TEXT NOT NULL DEFAULT 'active',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	expires_at DATETIME,
	rotated_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_bind_keys_hash ON bind_keys(key_hash);

CREATE TABLE IF NOT EXISTS user_hosts (
	user_id  INTEGER NOT NULL,
	host_id  INTEGER NOT NULL,
	key_id   INTEGER NOT NULL,
	bound_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (user_id, host_id)
);

CREATE TABLE IF NOT EXISTS user_servers (
	user_id   INTEGER NOT NULL,
	server_id INTEGER NOT NULL,
	key_id    INTEGER NOT NULL,
	bound_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (user_id, server_id)
);

CREATE TABLE IF NOT EXISTS servers (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	host_id     INTEGER NOT NULL,
	name        TEXT NOT NULL,
	domain_name TEXT NOT NULL,
	ssh_port    INTEGER NOT NULL DEFAULT 0,
	ssh_user    TEXT NOT NULL DEFAULT 'root',
	vcpu        INTEGER NOT NULL DEFAULT 0,
	mem_mb      INTEGER NOT NULL DEFAULT 0,
	UNIQUE (host_id, domain_name)
);

CREATE TABLE IF NOT EXISTS metrics (
	server_id   INTEGER NOT NULL,
	ts          DATETIME NOT NULL,
	cpu         REAL NOT NULL DEFAULT 0,
	mem_used_mb INTEGER NOT NULL DEFAULT 0,
	mem_total_mb INTEGER NOT NULL DEFAULT 0,
	disk_used   INTEGER NOT NULL DEFAULT 0,
	disk_total  INTEGER NOT NULL DEFAULT 0,
	net_rx_rate INTEGER NOT NULL DEFAULT 0,
	net_tx_rate INTEGER NOT NULL DEFAULT 0,
	net_rx      INTEGER NOT NULL DEFAULT 0,
	net_tx      INTEGER NOT NULL DEFAULT 0,
	uptime      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_metrics_server_ts ON metrics(server_id, ts);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ssh_host_keys (
	host     TEXT PRIMARY KEY,
	key      TEXT NOT NULL,
	added_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS audit_logs (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id   INTEGER NOT NULL DEFAULT 0,
	host_id   INTEGER NOT NULL DEFAULT 0,
	server_id INTEGER NOT NULL DEFAULT 0,
	action    TEXT NOT NULL,
	detail    TEXT NOT NULL DEFAULT '',
	result    TEXT NOT NULL DEFAULT '',
	ip        TEXT NOT NULL DEFAULT '',
	ts        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS alerts (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	kind        TEXT NOT NULL,
	level       TEXT NOT NULL DEFAULT 'warn',
	host_id     INTEGER NOT NULL DEFAULT 0,
	server_id   INTEGER NOT NULL DEFAULT 0,
	target      TEXT NOT NULL DEFAULT '',
	message     TEXT NOT NULL DEFAULT '',
	value       REAL NOT NULL DEFAULT 0,
	status      TEXT NOT NULL DEFAULT 'active',
	count       INTEGER NOT NULL DEFAULT 1,
	created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	resolved_at DATETIME,
	ack_at      DATETIME
);
CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_alerts_open ON alerts(kind, host_id, server_id) WHERE status = 'active';
`

// 老库升级时补列，报"已存在"直接忽略。
var migrations = []string{
	`ALTER TABLE audit_logs ADD COLUMN detail TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE audit_logs ADD COLUMN ip TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE bind_keys ADD COLUMN server_id INTEGER NOT NULL DEFAULT 0`,
}
