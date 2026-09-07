// Copyright (C) 2026 ABLESTACK
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	skydiveconfig "github.com/skydive-project/skydive/config"
)

func testConfig(path string) Config {
	return Config{
		Driver:      "sqlite3",
		Path:        path,
		JournalMode: "WAL",
		BusyTimeout: 3210,
	}
}

func TestConfigFromGlobal(t *testing.T) {
	global := skydiveconfig.GetConfig()
	old := ConfigFromGlobal()
	defer func() {
		global.Set("custom.database.driver", old.Driver)
		global.Set("custom.database.path", old.Path)
		global.Set("custom.database.journalMode", old.JournalMode)
		global.Set("custom.database.busyTimeout", old.BusyTimeout)
	}()

	global.Set("custom.database.driver", "sqlite3")
	global.Set("custom.database.path", "/tmp/configured-netdive.db")
	global.Set("custom.database.journalMode", "WAL")
	global.Set("custom.database.busyTimeout", 4321)

	got := ConfigFromGlobal()
	if got.Driver != "sqlite3" || got.Path != "/tmp/configured-netdive.db" || got.JournalMode != "WAL" || got.BusyTimeout != 4321 {
		t.Fatalf("unexpected custom.database config: %+v", got)
	}
}

func TestOpenCreatesAndMigratesFallbackDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netdive.db")
	db, err := Open(context.Background(), testConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for pragma, want := range map[string]string{
		"journal_mode": "wal",
		"synchronous":  "1",
		"foreign_keys": "1",
		"busy_timeout": "3210",
	} {
		var got string
		if err := db.SQLDB().QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", pragma, err)
		}
		if got != want {
			t.Errorf("PRAGMA %s = %q, want %q", pragma, got, want)
		}
	}

	assertSchema(t, db.SQLDB())
	version, err := db.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("schema version = %d, want 1", version)
	}
}

func TestReopenPreservesDataAndMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netdive.db")
	cfg := testConfig(path)

	first, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = first.SQLDB().Exec(`INSERT INTO manual_port_mapping
		(switch_node_id, switch_name, switch_port_node_id, host_node_id, host_nic_node_id)
		VALUES ('switch-id', 'switch-a', 'port-id', 'host-id', 'nic-id')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	var count int
	if err := second.SQLDB().QueryRow("SELECT COUNT(*) FROM manual_port_mapping").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("mapping count = %d, want 1", count)
	}
	if err := second.SQLDB().QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("migration record count = %d, want 1", count)
	}
}

func TestAdoptsCompatiblePrecreatedTemplateSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netdive.db")
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range migrations[0].statements {
		if _, err := raw.Exec(statement); err != nil {
			raw.Close()
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(context.Background(), testConfig(path))
	if err != nil {
		t.Fatalf("open compatible precreated template: %v", err)
	}
	defer db.Close()
	version, err := db.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("schema version = %d, want 1", version)
	}
}

func TestRejectsIncompatiblePrecreatedTemplateSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netdive.db")
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("CREATE TABLE manual_port_mapping (id INTEGER PRIMARY KEY)"); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), testConfig(path)); err == nil {
		t.Fatal("expected incompatible precreated schema to fail")
	}
}

func TestActiveMappingUniquenessAndDisabledHistory(t *testing.T) {
	db, err := Open(context.Background(), testConfig(filepath.Join(t.TempDir(), "netdive.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	insert := `INSERT INTO manual_port_mapping
		(switch_node_id, switch_port_node_id, host_node_id, host_nic_node_id, enabled)
		VALUES (?, ?, ?, ?, ?)`
	if _, err := db.SQLDB().Exec(insert, "switch-1", "port-1", "host-1", "nic-1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQLDB().Exec(insert, "switch-2", "port-1", "host-2", "nic-2", 1); err == nil {
		t.Error("expected duplicate active switch port to fail")
	}
	if _, err := db.SQLDB().Exec(insert, "switch-2", "port-2", "host-2", "nic-1", 1); err == nil {
		t.Error("expected duplicate active host NIC to fail")
	}
	if _, err := db.SQLDB().Exec(insert, "switch-2", "port-1", "host-2", "nic-1", 0); err != nil {
		t.Fatalf("disabled history should be allowed: %v", err)
	}
}

func TestManualPortMappingRepositoryCRUDAndFilters(t *testing.T) {
	db, err := Open(context.Background(), testConfig(filepath.Join(t.TempDir(), "netdive.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	created, err := db.CreateManualPortMapping(context.Background(), ManualPortMapping{
		SwitchNodeID: "switch-1", SwitchName: "Switch 1",
		SwitchPortNodeID: "port-1", SwitchPortName: "Ethernet1",
		HostNodeID: "host-1", HostName: "Host 1",
		HostNICNodeID: "nic-1", HostNICName: "eno1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID <= 0 || created.CreatedAt == "" || !created.Enabled {
		t.Fatalf("unexpected created mapping: %+v", created)
	}

	bySwitch, err := db.ListManualPortMappings(context.Background(), ManualPortMappingFilter{SwitchNodeID: "switch-1"})
	if err != nil || len(bySwitch) != 1 {
		t.Fatalf("switch filter returned %+v, %v", bySwitch, err)
	}
	byHostNIC, err := db.ListManualPortMappings(context.Background(), ManualPortMappingFilter{HostNodeID: "host-1", HostNICNodeID: "nic-1"})
	if err != nil || len(byHostNIC) != 1 {
		t.Fatalf("host/NIC filter returned %+v, %v", byHostNIC, err)
	}

	created.SwitchPortNodeID = "port-2"
	created.SwitchPortName = "Ethernet2"
	updated, err := db.UpdateManualPortMapping(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	if updated.SwitchPortNodeID != "port-2" || updated.CreatedAt != created.CreatedAt {
		t.Fatalf("unexpected updated mapping: %+v", updated)
	}

	if err := db.DisableManualPortMapping(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	active, err := db.ListManualPortMappings(context.Background(), ManualPortMappingFilter{})
	if err != nil || len(active) != 0 {
		t.Fatalf("active mappings returned %+v, %v", active, err)
	}
	all, err := db.ListManualPortMappings(context.Background(), ManualPortMappingFilter{IncludeDisabled: true})
	if err != nil || len(all) != 1 || all[0].Enabled {
		t.Fatalf("all mappings returned %+v, %v", all, err)
	}
	if err := db.DisableManualPortMapping(context.Background(), 9999); !errors.Is(err, ErrManualPortMappingNotFound) {
		t.Fatalf("missing disable error = %v", err)
	}
}

func TestManualPortMappingRepositoryReturnsConflict(t *testing.T) {
	db, err := Open(context.Background(), testConfig(filepath.Join(t.TempDir(), "netdive.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	base := ManualPortMapping{
		SwitchNodeID: "switch-1", SwitchPortNodeID: "port-1",
		HostNodeID: "host-1", HostNICNodeID: "nic-1", Enabled: true,
	}
	if _, err := db.CreateManualPortMapping(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	base.HostNICNodeID = "nic-2"
	if _, err := db.CreateManualPortMapping(context.Background(), base); !errors.Is(err, ErrManualPortMappingConflict) {
		t.Fatalf("duplicate port error = %v", err)
	}
}

func TestNewerSchemaVersionIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netdive.db")
	cfg := testConfig(path)
	db, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQLDB().Exec("INSERT INTO schema_migrations (version, name) VALUES (99, 'future')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), cfg); err == nil {
		t.Fatal("expected a newer schema version to be rejected")
	}
}

func TestOpenReportsInvalidPath(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "missing", "netdive.db"))
	if _, err := Open(context.Background(), cfg); err == nil {
		t.Fatal("expected invalid parent path to fail")
	}
}

func assertSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, object := range []string{
		"schema_migrations",
		"manual_port_mapping",
		"ux_manual_port_mapping_active_switch_port",
		"ux_manual_port_mapping_active_host_nic",
		"ix_manual_port_mapping_switch",
		"ix_manual_port_mapping_host_nic",
	} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = ?", object).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("schema object %q count = %d, want 1", object, count)
		}
	}
}
