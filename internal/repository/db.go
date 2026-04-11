package repository

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// DB es el wrapper del pool de conexiones SQLite.
type DB struct {
	*sql.DB
}

// New abre (o crea) la base de datos SQLite y aplica el esquema.
func New(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("abrir SQLite: %w", err)
	}

	// WAL mode para mejor concurrencia de lectura
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, err
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migración: %w", err)
	}

	return &DB{db}, nil
}

// migrate aplica el esquema inicial (idempotente).
func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS base_vms (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		name          TEXT    NOT NULL UNIQUE,
		description   TEXT    NOT NULL DEFAULT '',
		state         TEXT    NOT NULL DEFAULT 'stopped',
		has_root_keys INTEGER NOT NULL DEFAULT 0,
		root_priv_key BLOB,               -- PEM de la llave privada de root (cifrado en producción)
		root_pub_key  TEXT,
		vbox_uuid     TEXT,
		deletion_password TEXT NOT NULL DEFAULT '',
		created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS disks (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		base_vm_id  INTEGER NOT NULL REFERENCES base_vms(id) ON DELETE CASCADE,
		name        TEXT    NOT NULL,
		file_path   TEXT    NOT NULL UNIQUE,
		state       TEXT    NOT NULL DEFAULT 'no_keys',
		created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS user_vms (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		disk_id       INTEGER NOT NULL REFERENCES disks(id) ON DELETE CASCADE,
		name          TEXT    NOT NULL UNIQUE,
		description   TEXT    NOT NULL DEFAULT '',
		username      TEXT    NOT NULL DEFAULT '',
		state         TEXT    NOT NULL DEFAULT 'stopped',
		has_user_keys INTEGER NOT NULL DEFAULT 0,
		user_priv_key BLOB,
		user_pub_key  TEXT,
		vbox_uuid     TEXT,
		ssh_port      INTEGER,
		deletion_password TEXT NOT NULL DEFAULT '',
		created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Migraciones incrementales automáticas (SQLite ignora si ya existe con IF NOT EXISTS, pero ADD COLUMN requiere cuidado)
	_, _ = db.Exec("ALTER TABLE base_vms ADD COLUMN deletion_password TEXT NOT NULL DEFAULT ''")
	_, _ = db.Exec("ALTER TABLE user_vms ADD COLUMN deletion_password TEXT NOT NULL DEFAULT ''")

	return nil
}
