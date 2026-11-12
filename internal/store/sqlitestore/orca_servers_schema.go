//go:build sqlite || sqliteonly

package sqlitestore

import "database/sql"

const sqliteOrcaServersMigration = `CREATE TABLE IF NOT EXISTS orca_servers (
    id                 TEXT PRIMARY KEY,
    tenant_id          TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    label              VARCHAR(32) NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    pairing_code       BLOB NOT NULL,
    last_status        VARCHAR(16) NOT NULL DEFAULT 'unknown',
    last_runtime_state VARCHAR(64) NOT NULL DEFAULT '',
    last_error         TEXT NOT NULL DEFAULT '',
    last_checked_at    TEXT,
    last_healthy_at    TEXT,
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_by         VARCHAR(255) NOT NULL DEFAULT '',
    UNIQUE (tenant_id, label)
);
CREATE INDEX IF NOT EXISTS idx_orca_servers_checked ON orca_servers (last_checked_at);`

func sqliteOrcaServerAgentsMigrationPatch(db *sql.DB) (string, error) {
	columns := []struct{ name, def string }{
		{"agents", "TEXT NOT NULL DEFAULT '[]'"},
		{"agent_health", "TEXT NOT NULL DEFAULT '{}'"},
	}
	patch := ""
	for _, c := range columns {
		has, err := sqliteColumnExists(db, "orca_servers", c.name)
		if err != nil {
			return "", err
		}
		if !has {
			patch += "ALTER TABLE orca_servers ADD COLUMN " + c.name + " " + c.def + ";\n"
		}
	}
	if patch == "" {
		patch = "SELECT 1;"
	}
	return patch, nil
}
