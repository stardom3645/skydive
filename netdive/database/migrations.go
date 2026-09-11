// Copyright (C) 2026 ABLESTACK
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package database

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/skydive-project/skydive/graffiti/logging"
)

type migration struct {
	version    int
	name       string
	statements []string
	validate   func(context.Context, *sql.Tx) error
}

var migrations = []migration{
	{
		version: 1,
		name:    "create manual port mappings",
		statements: []string{
			`CREATE TABLE IF NOT EXISTS manual_port_mapping (
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
			`CREATE UNIQUE INDEX IF NOT EXISTS ux_manual_port_mapping_active_switch_port
				ON manual_port_mapping (switch_port_node_id) WHERE enabled = 1`,
			`CREATE UNIQUE INDEX IF NOT EXISTS ux_manual_port_mapping_active_host_nic
				ON manual_port_mapping (host_nic_node_id) WHERE enabled = 1`,
			`CREATE INDEX IF NOT EXISTS ix_manual_port_mapping_switch
				ON manual_port_mapping (switch_node_id, switch_port_node_id)`,
			`CREATE INDEX IF NOT EXISTS ix_manual_port_mapping_host_nic
				ON manual_port_mapping (host_node_id, host_nic_node_id)`,
		},
		validate: validateManualPortMappingSchema,
	},
	{
		version: 2,
		name:    "allow topology-free manual switch ports",
		statements: []string{
			`CREATE TABLE manual_port_mapping_v2 (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				switch_node_id TEXT NOT NULL,
				switch_name TEXT,
				switch_port_node_id TEXT,
				switch_port_name TEXT NOT NULL CHECK (length(trim(switch_port_name)) > 0),
				host_node_id TEXT NOT NULL,
				host_name TEXT,
				host_nic_node_id TEXT NOT NULL,
				host_nic_name TEXT,
				enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
				created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
				updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			)`,
			`INSERT INTO manual_port_mapping_v2 (
				id, switch_node_id, switch_name, switch_port_node_id, switch_port_name,
				host_node_id, host_name, host_nic_node_id, host_nic_name, enabled, created_at, updated_at
			) SELECT id, switch_node_id, switch_name, switch_port_node_id,
				COALESCE(NULLIF(trim(switch_port_name), ''), switch_port_node_id),
				host_node_id, host_name, host_nic_node_id, host_nic_name, enabled, created_at, updated_at
				FROM manual_port_mapping`,
			`UPDATE manual_port_mapping_v2 SET enabled = 0
				WHERE enabled = 1 AND id NOT IN (
					SELECT MAX(id) FROM manual_port_mapping_v2 WHERE enabled = 1
					GROUP BY switch_node_id, switch_port_name COLLATE NOCASE
				)`,
			`DROP TABLE manual_port_mapping`,
			`ALTER TABLE manual_port_mapping_v2 RENAME TO manual_port_mapping`,
			`CREATE UNIQUE INDEX ux_manual_port_mapping_active_switch_port_name
				ON manual_port_mapping (switch_node_id, switch_port_name COLLATE NOCASE) WHERE enabled = 1`,
			`CREATE UNIQUE INDEX ux_manual_port_mapping_active_host_nic
				ON manual_port_mapping (host_nic_node_id) WHERE enabled = 1`,
			`CREATE INDEX ix_manual_port_mapping_switch
				ON manual_port_mapping (switch_node_id, switch_port_name)`,
			`CREATE INDEX ix_manual_port_mapping_host_nic
				ON manual_port_mapping (host_node_id, host_nic_node_id)`,
		},
		validate: validateManualPortMappingSchemaV2,
	},
	{
		version: 3,
		name:    "track manual mapping disable reason",
		statements: []string{
			`ALTER TABLE manual_port_mapping ADD COLUMN disabled_reason TEXT`,
			`UPDATE manual_port_mapping SET disabled_reason = 'legacy_disabled'
				WHERE enabled = 0 AND disabled_reason IS NULL`,
		},
		validate: validateManualPortMappingSchemaV3,
	},
}

func init() {
	migrations = append(migrations, migration{version: 4, name: "create change event history", statements: []string{
		`CREATE TABLE event_history (id INTEGER PRIMARY KEY AUTOINCREMENT, resource_type TEXT NOT NULL, resource_id TEXT NOT NULL, resource_name TEXT NOT NULL DEFAULT '', event_type TEXT NOT NULL, old_value TEXT NOT NULL DEFAULT '', new_value TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '', severity TEXT NOT NULL DEFAULT '', metadata TEXT NOT NULL DEFAULT '{}', occurred_at INTEGER NOT NULL)`,
		`CREATE INDEX ix_event_time ON event_history(occurred_at, id)`,
		`CREATE INDEX ix_event_resource_type ON event_history(resource_type, occurred_at)`,
		`CREATE INDEX ix_event_resource_id ON event_history(resource_id, occurred_at)`,
		`CREATE INDEX ix_event_type ON event_history(event_type, occurred_at)`,
	}})
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
	if migration.validate != nil {
		if err := migration.validate(ctx, tx); err != nil {
			return fmt.Errorf("schema validation failed: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name) VALUES (?, ?)",
		migration.version, migration.name,
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	logging.GetLogger().Infof("Netdive migration %d applied: %s", migration.version, migration.name)
	return nil
}

func validateManualPortMappingSchema(ctx context.Context, tx *sql.Tx) error {
	return validateManualPortMappingSchemaVersion(ctx, tx, map[string]indexRequirement{
		"ux_manual_port_mapping_active_switch_port": {unique: 1, partial: 1, columns: []string{"switch_port_node_id"}},
		"ux_manual_port_mapping_active_host_nic":    {unique: 1, partial: 1, columns: []string{"host_nic_node_id"}},
		"ix_manual_port_mapping_switch":             {columns: []string{"switch_node_id", "switch_port_node_id"}},
		"ix_manual_port_mapping_host_nic":           {columns: []string{"host_node_id", "host_nic_node_id"}},
	}, nil)
}

func validateManualPortMappingSchemaV2(ctx context.Context, tx *sql.Tx) error {
	return validateManualPortMappingSchemaVersion(ctx, tx, map[string]indexRequirement{
		"ux_manual_port_mapping_active_switch_port_name": {unique: 1, partial: 1, columns: []string{"switch_node_id", "switch_port_name"}},
		"ux_manual_port_mapping_active_host_nic":         {unique: 1, partial: 1, columns: []string{"host_nic_node_id"}},
		"ix_manual_port_mapping_switch":                  {columns: []string{"switch_node_id", "switch_port_name"}},
		"ix_manual_port_mapping_host_nic":                {columns: []string{"host_node_id", "host_nic_node_id"}},
	}, map[string]int{"switch_port_node_id": 0, "switch_port_name": 1})
}

func validateManualPortMappingSchemaV3(ctx context.Context, tx *sql.Tx) error {
	return validateManualPortMappingSchemaVersion(ctx, tx, map[string]indexRequirement{
		"ux_manual_port_mapping_active_switch_port_name": {unique: 1, partial: 1, columns: []string{"switch_node_id", "switch_port_name"}},
		"ux_manual_port_mapping_active_host_nic":         {unique: 1, partial: 1, columns: []string{"host_nic_node_id"}},
		"ix_manual_port_mapping_switch":                  {columns: []string{"switch_node_id", "switch_port_name"}},
		"ix_manual_port_mapping_host_nic":                {columns: []string{"host_node_id", "host_nic_node_id"}},
	}, map[string]int{"switch_port_node_id": 0, "switch_port_name": 1}, []string{"disabled_reason"})
}

type indexRequirement struct {
	unique  int
	partial int
	columns []string
}

func validateManualPortMappingSchemaVersion(ctx context.Context, tx *sql.Tx, requiredIndexes map[string]indexRequirement, requiredNotNull map[string]int, additionalColumns ...[]string) error {
	requiredColumns := map[string]bool{
		"id": false, "switch_node_id": false, "switch_name": false,
		"switch_port_node_id": false, "switch_port_name": false,
		"host_node_id": false, "host_name": false,
		"host_nic_node_id": false, "host_nic_name": false,
		"enabled": false, "created_at": false, "updated_at": false,
	}
	for _, columns := range additionalColumns {
		for _, column := range columns {
			requiredColumns[column] = false
		}
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info(manual_port_mapping)")
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if _, required := requiredColumns[name]; required {
			requiredColumns[name] = true
		}
		if expected, required := requiredNotNull[name]; required && notNull != expected {
			rows.Close()
			return fmt.Errorf("manual_port_mapping column %q has incompatible nullability", name)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for name, found := range requiredColumns {
		if !found {
			return fmt.Errorf("manual_port_mapping is missing required column %q", name)
		}
	}

	indexRows, err := tx.QueryContext(ctx, "PRAGMA index_list(manual_port_mapping)")
	if err != nil {
		return err
	}
	foundIndexes := make(map[string]indexRequirement)
	for indexRows.Next() {
		var sequence, unique, partial int
		var name, origin string
		if err := indexRows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
			indexRows.Close()
			return err
		}
		if required, ok := requiredIndexes[name]; ok {
			required.unique = unique
			required.partial = partial
			foundIndexes[name] = required
		}
	}
	if err := indexRows.Close(); err != nil {
		return err
	}

	for name, required := range requiredIndexes {
		found, ok := foundIndexes[name]
		if !ok || found.unique != required.unique || found.partial != required.partial {
			return fmt.Errorf("manual_port_mapping index %q is missing or incompatible", name)
		}
		columnRows, err := tx.QueryContext(ctx, "PRAGMA index_info("+name+")")
		if err != nil {
			return err
		}
		var columns []string
		for columnRows.Next() {
			var sequence, cid int
			var column string
			if err := columnRows.Scan(&sequence, &cid, &column); err != nil {
				columnRows.Close()
				return err
			}
			columns = append(columns, column)
		}
		if err := columnRows.Close(); err != nil {
			return err
		}
		if len(columns) != len(required.columns) {
			return fmt.Errorf("manual_port_mapping index %q has incompatible columns", name)
		}
		for i := range columns {
			if columns[i] != required.columns[i] {
				return fmt.Errorf("manual_port_mapping index %q has incompatible columns", name)
			}
		}
	}
	return nil
}
