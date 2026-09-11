package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteRuntimeVersion(t *testing.T) {
	d := historyDB(t)
	var version string
	if err := d.db.QueryRow("SELECT sqlite_version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Logf("SQLite runtime: %s", version)
	if expected := os.Getenv("NETDIVE_EXPECT_SQLITE_VERSION"); expected != "" && version != expected {
		t.Fatalf("SQLite %s, expected %s", version, expected)
	}
}

func historyDB(t *testing.T) *Database {
	t.Helper()
	d, err := Open(context.Background(), testConfig(filepath.Join(t.TempDir(), "netdive.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestManualEventsExactlyOnceAndPersistence(t *testing.T) {
	d := historyDB(t)
	ctx := context.Background()
	m, err := d.CreateManualPortMapping(ctx, ManualPortMapping{SwitchNodeID: "switch", SwitchPortName: "xg1", HostNodeID: "host", HostNICNodeID: "nic", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.UpdateManualPortMapping(ctx, m); err != nil {
		t.Fatal(err)
	}
	m.SwitchPortName = "xg2"
	if _, err = d.UpdateManualPortMapping(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err = d.DisableManualPortMapping(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if err = d.DisableManualPortMapping(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	d.events.close() // drain writes before asserting the persisted result
	page, err := d.ListEvents(ctx, EventFilter{})
	if err != nil || page.Total != 3 {
		t.Fatalf("events: %+v, %v", page, err)
	}
	for i, kind := range []string{"manual_mapping_deleted", "manual_mapping_updated", "manual_mapping_created"} {
		if page.Events[i].EventType != kind {
			t.Fatal(page.Events)
		}
	}
	path := d.path
	d.Close()
	reopened, err := Open(ctx, testConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	page, err = reopened.ListEvents(ctx, EventFilter{})
	if err != nil || page.Total != 3 {
		t.Fatal(page, err)
	}
}

func TestEventFilteringPaginationAndRetention(t *testing.T) {
	d := historyDB(t)
	ctx := context.Background()
	now := time.Now().Unix()
	for _, e := range []ChangeEvent{
		{ResourceType: "nic", ResourceID: "a", ResourceName: "eno1", EventType: "link_changed", Source: "infrastructure", OccurredAt: now},
		{ResourceType: "nic", ResourceID: "b", ResourceName: "eno2", EventType: "link_changed", Source: "infrastructure", OccurredAt: now},
		{ResourceType: "pod", ResourceID: "p", ResourceName: "api-pod", EventType: "state_changed", Source: "kubernetes", OccurredAt: now},
		{ResourceType: "nic", ResourceID: "old", EventType: "link_changed", OccurredAt: now - 31*86400},
	} {
		if err := d.insertEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	page, err := d.ListEvents(ctx, EventFilter{ResourceType: "nic", Source: "infrastructure", Search: "eno", EventType: "link_changed", Page: 2, PageSize: 1})
	if err != nil || page.Total != 2 || len(page.Events) != 1 || page.Events[0].ResourceID != "a" {
		t.Fatal(page, err)
	}
	page, err = d.ListEvents(ctx, EventFilter{ResourceID: "p"})
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
	n, err := d.CleanupEvents(ctx, now-30*86400)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	page, err = d.ListEvents(ctx, EventFilter{From: now - 40*86400})
	if err != nil || page.Total != 3 {
		t.Fatal(page, err)
	}
}

func TestEventFailureDoesNotFailManualCRUD(t *testing.T) {
	d := historyDB(t)
	ctx := context.Background()
	if _, err := d.db.Exec("DROP TABLE event_history"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for i := 0; i < 2000; i++ {
		d.RecordEvent(ChangeEvent{ResourceType: "nic", ResourceID: "n", EventType: "link_changed"})
	}
	if time.Since(start) > time.Second {
		t.Fatal("event enqueue blocked")
	}
	_, err := d.CreateManualPortMapping(ctx, ManualPortMapping{SwitchNodeID: "s", SwitchPortName: "p", HostNodeID: "h", HostNICNodeID: "n", Enabled: true})
	if err != nil {
		t.Fatal("event failure broke mapping CRUD", err)
	}
}

func TestLockedDatabaseDoesNotBlockEventProducer(t *testing.T) {
	d := historyDB(t)
	locker, err := sql.Open("sqlite3", d.path)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	if _, err = locker.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	d.RecordEvent(ChangeEvent{ResourceType: "nic", ResourceID: "id", EventType: "link_changed", OldValue: "UP", NewValue: "DOWN"})
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("SQLite lock blocked producer")
	}
	if _, err = locker.Exec("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	d.events.close()
	page, err := d.ListEvents(context.Background(), EventFilter{})
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
}
