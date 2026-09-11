// Copyright (C) 2026 ABLESTACK
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Package database provides the shared local SQLite database used by
// ABLESTACK-specific Netdive features. It is intentionally independent from
// topology storage, Mold's MySQL database, and Wall's SQLite database.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"

	"github.com/skydive-project/skydive/config"
	"github.com/skydive-project/skydive/graffiti/logging"
)

const sqliteDriver = "sqlite3"

// Config describes Netdive's local database settings.
type Config struct {
	Driver                    string
	Path                      string
	JournalMode               string
	BusyTimeout               int
	EventHistoryRetentionDays int
}

// Database is the common access point for Netdive's local persistent data.
// Later manual-mapping repositories should share this handle rather than open
// feature-specific connections.
type Database struct {
	db       *sql.DB
	path     string
	events   *eventWriter
	manualMu sync.Mutex
}

// ConfigFromGlobal reads the ABLESTACK-specific custom.database section.
func ConfigFromGlobal() Config {
	return Config{
		Driver:                    config.GetString("custom.database.driver"),
		Path:                      config.GetString("custom.database.path"),
		JournalMode:               config.GetString("custom.database.journalMode"),
		BusyTimeout:               config.GetInt("custom.database.busyTimeout"),
		EventHistoryRetentionDays: config.GetInt("custom.database.eventHistoryRetentionDays"),
	}
}

// OpenFromConfig opens and migrates the configured Netdive database. An empty
// driver means this ABLESTACK extension is disabled.
func OpenFromConfig(ctx context.Context) (*Database, error) {
	cfg := ConfigFromGlobal()
	if strings.TrimSpace(cfg.Driver) == "" {
		return nil, nil
	}
	return Open(ctx, cfg)
}

// Open opens an existing template database, or creates it as a warned fallback,
// applies connection policy, and runs all pending migrations.
func Open(ctx context.Context, cfg Config) (*Database, error) {
	if cfg.Driver != sqliteDriver {
		return nil, fmt.Errorf("unsupported custom.database.driver %q (expected %q)", cfg.Driver, sqliteDriver)
	}
	if strings.TrimSpace(cfg.Path) == "" {
		return nil, fmt.Errorf("custom.database.path must not be empty")
	}
	if strings.ToUpper(strings.TrimSpace(cfg.JournalMode)) != "WAL" {
		return nil, fmt.Errorf("unsupported custom.database.journalMode %q (Netdive requires WAL)", cfg.JournalMode)
	}
	if cfg.BusyTimeout < 0 {
		return nil, fmt.Errorf("custom.database.busyTimeout must not be negative")
	}

	if _, err := os.Stat(cfg.Path); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("cannot inspect Netdive database %q: %w", cfg.Path, err)
		}
		logging.GetLogger().Warningf("Netdive database %q is missing; creating fallback database (the CCVM template should normally provide this file)", cfg.Path)
	}

	query := url.Values{}
	query.Set("_busy_timeout", fmt.Sprintf("%d", cfg.BusyTimeout))
	query.Set("_foreign_keys", "on")
	query.Set("_journal_mode", strings.ToUpper(strings.TrimSpace(cfg.JournalMode)))
	query.Set("_synchronous", "NORMAL")
	dsn := (&url.URL{Scheme: "file", Path: cfg.Path, RawQuery: query.Encode()}).String()

	sqlDB, err := sql.Open(sqliteDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("cannot configure Netdive database %q: %w", cfg.Path, err)
	}
	// A single shared connection makes connection-scoped PRAGMAs deterministic
	// and avoids unnecessary competing writers for this low-write database.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	db := &Database{db: sqlDB, path: cfg.Path}
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("cannot open Netdive database %q: %w", cfg.Path, err)
	}
	if err := db.verifyConnectionPolicy(ctx, cfg.BusyTimeout); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("cannot initialize Netdive database %q: %w", cfg.Path, err)
	}
	if err := db.migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("cannot migrate Netdive database %q: %w", cfg.Path, err)
	}
	version, err := db.SchemaVersion(ctx)
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("cannot read Netdive database schema version %q: %w", cfg.Path, err)
	}
	logging.GetLogger().Infof("Netdive database %q opened at schema version %d", cfg.Path, version)

	db.events = newEventWriter(db, cfg.EventHistoryRetentionDays)
	return db, nil
}

func (d *Database) verifyConnectionPolicy(ctx context.Context, busyTimeout int) error {
	checks := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"synchronous", "1"}, // SQLite value for NORMAL
		{"foreign_keys", "1"},
		{"busy_timeout", fmt.Sprintf("%d", busyTimeout)},
	}
	for _, check := range checks {
		var got string
		if err := d.db.QueryRowContext(ctx, "PRAGMA "+check.pragma).Scan(&got); err != nil {
			return fmt.Errorf("read PRAGMA %s: %w", check.pragma, err)
		}
		if strings.ToLower(got) != check.want {
			return fmt.Errorf("PRAGMA %s is %q, expected %q", check.pragma, got, check.want)
		}
	}
	return nil
}

// SQLDB exposes the shared handle for focused repository implementations.
// Callers must not close it; the analyzer owns the Database lifecycle.
func (d *Database) SQLDB() *sql.DB {
	return d.db
}

// SchemaVersion returns the highest successfully applied migration version.
func (d *Database) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := d.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

// Close closes the shared database handle.
func (d *Database) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	if d.events != nil {
		d.events.close()
	}
	return d.db.Close()
}
