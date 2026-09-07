// Copyright (C) 2026 ABLESTACK
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package database

import (
	"context"
	"database/sql"
	"fmt"
)

type migration struct {
	version    int
	name       string
	statements []string
}

var migrations = []migration{
	{
		version: 1,
		name:    "create manual port mappings",
		statements: []string{
			`CREATE TABLE manual_port_mapping (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				switch_node_id TEXT NOT NULL,
				switch_name TEXT,
				switch_port_node_id TEXT NOT NULL,
				switch_port_name TEXT,
				host_node_id TEXT NOT NULL,
				host_name TEXT,
				host_nic_node_id TEXT NOT NULL,
				host_nic_name TEXT,
				enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
				created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
				updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			)`,
			`CREATE UNIQUE INDEX ux_manual_port_mapping_active_switch_port
				ON manual_port_mapping (switch_port_node_id) WHERE enabled = 1`,
			`CREATE UNIQUE INDEX ux_manual_port_mapping_active_host_nic
				ON manual_port_mapping (host_nic_node_id) WHERE enabled = 1`,
			`CREATE INDEX ix_manual_port_mapping_switch
				ON manual_port_mapping (switch_node_id, switch_port_node_id)`,
			`CREATE INDEX ix_manual_port_mapping_host_nic
				ON manual_port_mapping (host_node_id, host_nic_node_id)`,
		},
	},
}

func (d *Database) migrate(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	latestVersion := 0
	for _, migration := range migrations {
		if migration.version <= latestVersion {
			return fmt.Errorf("migrations are not in strictly increasing order at version %d", migration.version)
		}
		latestVersion = migration.version
	}
	currentVersion, err := d.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("read current schema version: %w", err)
	}
	if currentVersion > latestVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", currentVersion, latestVersion)
	}

	for _, migration := range migrations {
		if err := d.applyMigration(ctx, migration); err != nil {
			return fmt.Errorf("migration %d (%s) failed: %w", migration.version, migration.name, err)
		}
	}
	return nil
}

func (d *Database) applyMigration(ctx context.Context, migration migration) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var version int
	err = tx.QueryRowContext(ctx, "SELECT version FROM schema_migrations WHERE version = ?", migration.version).Scan(&version)
	if err == nil {
		return tx.Commit()
	}
	if err != sql.ErrNoRows {
		return err
	}

	for _, statement := range migration.statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name) VALUES (?, ?)",
		migration.version, migration.name,
	); err != nil {
		return err
	}
	return tx.Commit()
}
